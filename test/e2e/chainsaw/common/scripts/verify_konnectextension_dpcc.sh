#!/bin/bash
# Verifies that a KonnectExtension owns exactly one Programmed KongDataPlaneClientCertificate
# registering the certificate it published.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: Namespace holding the KonnectExtension, its Secret and the certificate.
#   EXTENSION_NAME: Name of the KonnectExtension managing the certificate.
#   CONTROL_PLANE_NAME: KonnectGatewayControlPlane the certificate must reference.
#   EXPECTED_SECRET_PROVISIONING: (Optional) Required value of the
#     gateway.konghq.com/secret-provisioning label on the published Secret ("automatic"
#     for Automatic provisioning). Empty means the label is not checked.
#   EXPECTED_DPCC_COUNT: (Optional) Number of certificates the extension must own. Default: 1.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
EXTENSION_NAME="${EXTENSION_NAME}"
CONTROL_PLANE_NAME="${CONTROL_PLANE_NAME}"
EXPECTED_SECRET_PROVISIONING="${EXPECTED_SECRET_PROVISIONING:-}"
EXPECTED_DPCC_COUNT="${EXPECTED_DPCC_COUNT:-1}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

fail() {
  jq -n --arg error "$1" \
    --arg namespace "$NAMESPACE" \
    --arg extension "$EXTENSION_NAME" \
    --arg control_plane "$CONTROL_PLANE_NAME" \
    --arg secret "${secret_name:-}" \
    --arg extension_status "${extension_status:-}" \
    --arg certificates "${certificates:-}" \
    --arg uid "${extension_uid:-}" \
    --arg last_error "${last_error:-}" \
    '{
      success: false,
      error: $error,
      namespace: $namespace,
      extension: $extension,
      control_plane: $control_plane,
      published_secret: $secret,
      last_error: (if $last_error == "" then null else $last_error end),
      extension_status: (if $extension_status == "" then null else ($extension_status | fromjson) end),
      owned_certificates: (if $certificates == "" then [] else [$certificates | fromjson | .items[]
        | select(any(.metadata.ownerReferences[]?; .uid == $uid))] end),
      kubectl_commands: [
        "kubectl get konnectextension \($extension) -n \($namespace) -o json",
        "kubectl get kongdataplaneclientcertificates -n \($namespace) -o json"
      ]
    }'
  exit 1
}

secret_name=""
extension_status=""
extension_uid=""
certificates=""
last_error=""

for attempt in $(seq 1 "$RETRY_COUNT"); do
  if ! output=$(kubectl get konnectextension "$EXTENSION_NAME" -n "$NAMESPACE" -o json 2>&1); then
    last_error="$output"
    sleep "$RETRY_DELAY"
    continue
  fi

  extension_status="$output"
  extension_uid=$(jq -r '.metadata.uid' <<<"$extension_status")
  secret_name=$(jq -r '.status.dataPlaneClientAuth.certificateSecretRef.name // ""' <<<"$extension_status")
  if [[ -z "$secret_name" ]]; then
    last_error="extension has no published certificate Secret yet"
    sleep "$RETRY_DELAY"
    continue
  fi

  if ! secret=$(kubectl get secret "$secret_name" -n "$NAMESPACE" -o json 2>&1); then
    last_error="$secret"
    sleep "$RETRY_DELAY"
    continue
  fi
  if [[ -n "$EXPECTED_SECRET_PROVISIONING" ]]; then
    provisioning=$(jq -r '.metadata.labels["gateway.konghq.com/secret-provisioning"] // ""' <<<"$secret")
    if [[ "$provisioning" != "$EXPECTED_SECRET_PROVISIONING" ]]; then
      last_error="Secret ${secret_name} provisioning label is '${provisioning}'"
      sleep "$RETRY_DELAY"
      continue
    fi
  fi
  if ! output=$(kubectl get kongdataplaneclientcertificates -n "$NAMESPACE" -o json 2>&1); then
    last_error="$output"
    sleep "$RETRY_DELAY"
    continue
  fi

  certificates="$output"
  owned=$(jq -c --arg uid "$extension_uid" \
    '[.items[] | select(any(.metadata.ownerReferences[]?; .uid == $uid))]' <<<"$certificates")
  count=$(jq 'length' <<<"$owned")
  if [[ "$count" != "$EXPECTED_DPCC_COUNT" ]]; then
    last_error="extension owns ${count} certificates, expected ${EXPECTED_DPCC_COUNT}"
    sleep "$RETRY_DELAY"
    continue
  fi

  certificate=$(jq -c '.[0]' <<<"$owned")
  if ! jq -e \
    --arg ext "$EXTENSION_NAME" \
    --arg cp "$CONTROL_PLANE_NAME" \
    --arg secret_cert "$(jq -r '.data["tls.crt"]' <<<"$secret")" '
    def norm: gsub("\r"; "") | rtrimstr("\n");
    def programmed: [.status.conditions[]? | select(.type == "Programmed" and .status == "True")] | length > 0;
    def valid_cp_ref: [.status.conditions[]? | select(.type == "ControlPlaneRefValid" and .status == "True")] | length > 0;
    .metadata.deletionTimestamp == null and
    .metadata.name == $ext and
    .metadata.labels["gateway-operator.konghq.com/managed-by"] == "KonnectExtension" and
    .metadata.labels["gateway-operator.konghq.com/managed-by-name"] == $ext and
    (.spec.cert | norm) == ($secret_cert | @base64d | norm) and
    .spec.controlPlaneRef.type == "konnectNamespacedRef" and
    .spec.controlPlaneRef.konnectNamespacedRef.name == $cp and
    programmed and valid_cp_ref and
    (.status.konnect.id // "") != ""
  ' <<<"$certificate" >/dev/null 2>&1; then
    last_error="certificate $(jq -r '.metadata.name' <<<"$certificate") does not match the published Secret or is not Programmed"
    sleep "$RETRY_DELAY"
    continue
  fi

  jq -n \
    --arg namespace "$NAMESPACE" \
    --arg extension "$EXTENSION_NAME" \
    --arg secret "$secret_name" \
    --arg control_plane "$CONTROL_PLANE_NAME" \
    --arg certificate "$(jq -r '.metadata.name' <<<"$certificate")" \
    --arg konnect_id "$(jq -r '.status.konnect.id' <<<"$certificate")" \
    --arg konnect_cp_id "$(jq -r '.status.konnect.controlPlaneID' <<<"$certificate")" \
    --arg secret_provisioning "$(jq -r '.metadata.labels["gateway.konghq.com/secret-provisioning"] // ""' <<<"$secret")" \
    '{
      success: true,
      namespace: $namespace,
      extension: $extension,
      published_secret: $secret,
      secret_provisioning: (if $secret_provisioning == "" then null else $secret_provisioning end),
      control_plane: $control_plane,
      certificate: $certificate,
      konnect_id: $konnect_id,
      konnect_control_plane_id: $konnect_cp_id,
      kubectl_command: "kubectl get kongdataplaneclientcertificate \($certificate) -n \($namespace) -o json"
    }'
  exit 0
done

fail "the KongDataPlaneClientCertificate owned by KonnectExtension ${EXTENSION_NAME} was not provisioned as expected after ${RETRY_COUNT} attempts"
