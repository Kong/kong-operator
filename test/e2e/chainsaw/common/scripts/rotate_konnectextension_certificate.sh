#!/bin/bash
# Variables:
#   NAMESPACE, EXTENSION_NAME, DATAPLANE_NAME: resources under test.
#   MODE: manual (in-place renewal), regenerate (delete Automatic Secret),
#         or renewal (seed a legacy Automatic certificate inside the renewal window).
#   KONNECT_TOKEN, KONNECT_SERVER_URL: credentials and regional API endpoint.
#   MAX_RETRIES: optional, defaults to 180.
set -o errexit
set -o nounset
set -o pipefail

: "${NAMESPACE:?}" "${EXTENSION_NAME:?}" "${DATAPLANE_NAME:?}" "${MODE:?}" "${KONNECT_TOKEN:?}"
MAX_RETRIES="${MAX_RETRIES:-180}"
KONNECT_SERVER_URL="${KONNECT_SERVER_URL:-eu.api.konghq.tech}"
KONNECT_SERVER_URL="${KONNECT_SERVER_URL#https://}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
deployment=""
paused=false
cleanup() {
  if [[ "$paused" == true ]]; then
    kubectl rollout resume deployment "$deployment" -n "$NAMESPACE" >&2
  fi
  rm -f "$tmp/cert.json" "$tmp/extension.json" "$tmp/secret.json" "$tmp/key" "$tmp/crt"
  rmdir "$tmp"
}
trap cleanup EXIT

fail() {
  jq -n --arg error "$1" --arg namespace "$NAMESPACE" --arg extension "$EXTENSION_NAME" \
    '{success:false,error:$error,namespace:$namespace,extension:$extension}'
  exit 1
}

extension() {
  kubectl get konnectextension "$EXTENSION_NAME" -n "$NAMESPACE" -o json
}

registration_for_secret() {
  local secret="$1" pem
  pem="$(kubectl get secret "$secret" -n "$NAMESPACE" -o json | jq -r '.data["tls.crt"] | @base64d')"
  kubectl get kongdataplaneclientcertificates -n "$NAMESPACE" -o json |
    jq -c --arg uid "$extension_uid" --arg pem "$pem" \
      '[.items[] | select(any(.metadata.ownerReferences[]?; .uid == $uid)) |
        select((.spec.cert | gsub("\r"; "") | rtrimstr("\n")) == $pem)][0] // {}'
}

extension > "$tmp/extension.json"
extension_uid="$(jq -r '.metadata.uid' "$tmp/extension.json")"
old_secret="$(jq -r '.status.dataPlaneClientAuth.certificateSecretRef.name' "$tmp/extension.json")"
old_certificate="$(registration_for_secret "$old_secret" | jq -er '.metadata.name')"
dp_uid="$(kubectl get dataplane "$DATAPLANE_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
deployment="$(kubectl get deployment -n "$NAMESPACE" -o json |
  jq -er --arg uid "$dp_uid" '[.items[] | select(any(.metadata.ownerReferences[]?; .uid == $uid))][0].metadata.name')"
kubectl rollout status deployment "$deployment" -n "$NAMESPACE" --timeout=180s >&2
kubectl rollout pause deployment "$deployment" -n "$NAMESPACE" >&2
paused=true

case "$MODE" in
  manual)
    source="$(jq -r '.spec.clientAuth.certificateSecret.secretRef.name' "$tmp/extension.json")"
    source_uid="$(kubectl get secret "$source" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')"
    HOSTNAME="$EXTENSION_NAME.$NAMESPACE" bash "$SCRIPT_DIR/generate_tls_certificate.sh" > "$tmp/cert.json"
    jq --arg name "$source" --arg ns "$NAMESPACE" \
      '{apiVersion:"v1",kind:"Secret",metadata:{name:$name,namespace:$ns,
        labels:{"konghq.com/secret":"true","konghq.com/konnect-dp-cert":"true"}},
        type:"kubernetes.io/tls",stringData:{"tls.crt":.cert,"tls.key":.key}}' "$tmp/cert.json" > "$tmp/secret.json"
    kubectl apply -f "$tmp/secret.json" >&2
    [[ "$(kubectl get secret "$source" -n "$NAMESPACE" -o jsonpath='{.metadata.uid}')" == "$source_uid" ]] ||
      fail "Manual renewal replaced the source Secret instead of changing it in place"
    ;;
  regenerate)
    kubectl delete secret "$old_secret" -n "$NAMESPACE" --wait=false >&2
    ;;
  renewal)
    # A one-day legacy certificate is already inside the default seven-day
    # renewal margin. No shared operator flags or wall-clock wait are required.
    source="${EXTENSION_NAME}-expiring"
    sleep 2
    openssl req -x509 -nodes -days 1 -newkey rsa:2048 -keyout "$tmp/key" -out "$tmp/crt" \
      -subj "/CN=$EXTENSION_NAME.$NAMESPACE" >&2
    jq -n --arg name "$source" --arg ns "$NAMESPACE" --arg owner "$EXTENSION_NAME" --arg uid "$extension_uid" \
      --rawfile cert "$tmp/crt" --rawfile key "$tmp/key" \
      '{apiVersion:"v1",kind:"Secret",metadata:{name:$name,namespace:$ns,
        ownerReferences:[{apiVersion:"konnect.konghq.com/v1alpha2",kind:"KonnectExtension",
          name:$owner,uid:$uid,controller:true}],
        labels:{"konghq.com/secret":"true","konghq.com/konnect-dp-cert":"true",
          "gateway.konghq.com/secret-provisioning":"automatic",
          "gateway-operator.konghq.com/managed-by":"konnect-extension"}},
        type:"kubernetes.io/tls",immutable:true,stringData:{"tls.crt":$cert,"tls.key":$key}}' > "$tmp/secret.json"
    kubectl create -f "$tmp/secret.json" >&2
    ;;
  *) fail "Unknown rotation mode: $MODE" ;;
esac

new_secret=""
new_certificate=""
for attempt in $(seq 1 "$MAX_RETRIES"); do
  extension > "$tmp/extension.json"
  candidate="$(jq -r '.status.dataPlaneClientAuth.certificateSecretRef.name // ""' "$tmp/extension.json")"
  if [[ "$candidate" != "$old_secret" && -n "$candidate" ]]; then
    registration_for_secret "$candidate" > "$tmp/cert.json"
    if jq -e '.metadata.deletionTimestamp == null and (.status.konnect.id | length > 0) and
        any(.status.conditions[]?; .type == "Programmed" and .status == "True")' "$tmp/cert.json" >/dev/null &&
        jq -e 'any(.status.conditions[]?; .type == "Ready" and .status == "True")' "$tmp/extension.json" >/dev/null; then
      new_secret="$candidate"
      new_certificate="$(jq -r '.metadata.name' "$tmp/cert.json")"
      break
    fi
    # Publishing a generation while unready is allowed; consuming it is not.
    mounted="$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o json |
      jq -r '.spec.template.spec.volumes[] | select(.name == "kong-cluster-cert") | .secret.secretName')"
    if [[ "$mounted" == "$candidate" ]]; then
      registration_for_secret "$candidate" > "$tmp/cert.json"
      jq -e '(.status.konnect.id | length > 0) and .metadata.deletionTimestamp == null and
        any(.status.conditions[]?; .type == "Programmed" and .status == "True")' "$tmp/cert.json" >/dev/null ||
        fail "Deployment consumed an unregistered certificate"
    fi
  fi
  sleep 1
done
[[ -n "$new_secret" ]] || fail "Timed out waiting for a registered replacement certificate"

kubectl get kongdataplaneclientcertificate "$old_certificate" -n "$NAMESPACE" -o json |
  jq -e '.metadata.deletionTimestamp == null' >/dev/null ||
  fail "Previous certificate was retired while the Deployment was paused"
[[ "$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o jsonpath='{.spec.paused}')" == "true" ]] ||
  fail "The controller unexpectedly resumed the paused Deployment"
[[ "$(kubectl get secret "$new_secret" -n "$NAMESPACE" -o jsonpath='{.immutable}')" == "true" ]] ||
  fail "The published certificate generation is mutable"
if [[ "$MODE" == "manual" ]]; then
  [[ "$new_secret" != "$source" ]] || fail "Manual source Secret was published directly instead of a snapshot"
  [[ "$(kubectl get secret "$source" -n "$NAMESPACE" -o jsonpath='{.data.tls\.crt}')" == \
     "$(kubectl get secret "$new_secret" -n "$NAMESPACE" -o jsonpath='{.data.tls\.crt}')" ]] ||
    fail "Snapshot does not match the renewed source certificate"
fi

kubectl rollout resume deployment "$deployment" -n "$NAMESPACE" >&2
paused=false
kubectl rollout status deployment "$deployment" -n "$NAMESPACE" --timeout=180s >&2

if [[ "$MODE" == "renewal" ]]; then
  # Once consumers finish migrating to the seeded legacy generation, it must
  # itself be renewed from the actual NotAfter, not merely an annotation.
  for attempt in $(seq 1 "$MAX_RETRIES"); do
    candidate="$(extension | jq -r '.status.dataPlaneClientAuth.certificateSecretRef.name // ""')"
    if [[ -n "$candidate" && "$candidate" != "$source" && "$candidate" != "$old_secret" ]]; then
      new_secret="$candidate"
      new_certificate="$(registration_for_secret "$new_secret" | jq -er '.metadata.name')"
      break
    fi
    sleep 1
  done
  [[ "$new_secret" != "$source" ]] || fail "Automatic certificate was not renewed inside its expiration margin"
fi

for attempt in $(seq 1 "$MAX_RETRIES"); do
  certificates="$(kubectl get kongdataplaneclientcertificates -n "$NAMESPACE" -o json |
    jq -c --arg uid "$extension_uid" '[.items[] | select(any(.metadata.ownerReferences[]?; .uid == $uid))]')"
  if jq -e --arg name "$new_certificate" 'length == 1 and .[0].metadata.name == $name and
      .[0].metadata.deletionTimestamp == null and any(.[0].status.conditions[]?; .type == "Programmed" and .status == "True")' \
      <<< "$certificates" >/dev/null; then
    break
  fi
  sleep 1
done
jq -e --arg name "$new_certificate" 'length == 1 and .[0].metadata.name == $name' <<< "$certificates" >/dev/null ||
  fail "Stale certificate registrations were not cleaned up after rollout"
kubectl rollout status deployment "$deployment" -n "$NAMESPACE" --timeout=180s >&2
selector="$(kubectl get deployment "$deployment" -n "$NAMESPACE" -o json |
  jq -r '.spec.selector.matchLabels | to_entries | map(.key + "=" + .value) | join(",")')"
pods="$(kubectl get pod -n "$NAMESPACE" -l "$selector" -o json)"
jq -e --arg secret "$new_secret" '(.items | length == 2) and all(.items[];
  .metadata.deletionTimestamp == null and
  any(.status.conditions[]?; .type == "Ready" and .status == "True") and
  any(.spec.volumes[]; .name == "kong-cluster-cert" and .secret.secretName == $secret))' <<< "$pods" >/dev/null ||
  fail "Not all replicas are ready using the registered certificate"

cp_id="$(extension | jq -er '.status.konnect.controlPlaneID')"
cert_id="$(registration_for_secret "$new_secret" | jq -er '.status.konnect.id')"
hostnames="$(jq -c '[.items[].metadata.name]' <<< "$pods")"
for attempt in $(seq 1 "$MAX_RETRIES"); do
  nodes="$(curl --silent --show-error --fail --max-time 20 \
    -H "Authorization: Bearer $KONNECT_TOKEN" "https://$KONNECT_SERVER_URL/v2/control-planes/$cp_id/nodes")"
  if jq -e --arg cert "$cert_id" --argjson hosts "$hostnames" \
    '.items as $nodes | all($hosts[]; . as $host |
      any($nodes[]; .hostname == $host and .data_plane_cert_id == $cert and .connection_state.is_connected == true))' \
      <<< "$nodes" >/dev/null; then
    jq -n --arg mode "$MODE" --arg secret "$new_secret" --arg cert "$cert_id" --argjson pods "$hostnames" \
      '{success:true,mode:$mode,secret:$secret,konnectCertificateID:$cert,connectedPods:$pods}'
    exit 0
  fi
  sleep 1
done
fail "Fresh replicas are not connected to Konnect with the current registered certificate"
