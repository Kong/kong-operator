#!/usr/bin/env bash
# Usage: gw-admin-serves.sh GWADMIN GATEWAY_UID CODE
# Waits, up to $ATTEMPTS checks 2s apart (30 by default), for every running
# gw-admin pod of the GWAdmin, in $NAMESPACE, to answer CODE to a read of the
# entities of the gateway: 200 when it serves the gateway, 404 when it
# doesn't. A pod's readiness doesn't tell: it only turns Ready once every
# gateway it serves also has a data plane certificate, which another
# controller pushes. It reaches each pod on its API listener through a port
# forward, over mutual TLS, as Kong Operator does: verified with the API CA
# bundle of the GWAdmin, under the name of its API Service, and authenticated
# with the operator client certificate, kept in $OPERATOR_NAMESPACE
# (kong-system by default).
set -euo pipefail

ns="${NAMESPACE:?NAMESPACE must be set}"
operator_ns="${OPERATOR_NAMESPACE:-kong-system}"
gwadmin="$1" id="$2" want="$3"

work=$(mktemp -d)
pf=""
cleanup() {
  [ -z "$pf" ] || kill "$pf" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

secret_key() { # namespace secret key file
  kubectl get secret -n "$1" "$2" -o json | jq -er --arg k "$3" '.data[$k] | @base64d' > "$4"
}
gw=$(kubectl get gwadmin -n "$ns" "$gwadmin" -o json)
uid=$(jq -er .metadata.uid <<<"$gw")
svc=$(jq -er .status.api.serviceName <<<"$gw")
port=$(jq -er .status.api.port <<<"$gw")
secret_key "$ns" "$(jq -er .status.tls.apiCABundleSecretName <<<"$gw")" ca.crt "$work/ca.crt"
secret_key "$operator_ns" "gwadmin-$uid-operator-client-tls" tls.crt "$work/tls.crt"
secret_key "$operator_ns" "gwadmin-$uid-operator-client-tls" tls.key "$work/tls.key"
host="$svc.$ns.svc"

# check: whether every running pod answers CODE, printing why not.
check() {
pods=$(kubectl get pods -n "$ns" -o name --field-selector=status.phase=Running \
  -l "gateway-operator.konghq.com/managed-by=gw-admin,gateway-operator.konghq.com/managed-by-name=$gwadmin")
[ -n "$pods" ] || { echo "no running gw-admin pod of the GWAdmin $ns/$gwadmin"; return 1; }
for pod in $pods; do
  # A local port of the system's choice, read from the output.
  kubectl port-forward -n "$ns" "$pod" ":$port" > "$work/pf.log" 2>&1 &
  pf=$!
  local_port=""
  for _ in $(seq 1 50); do
    local_port=$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9]*\) .*/\1/p' "$work/pf.log" | head -n1)
    [ -n "$local_port" ] && break
    sleep 0.2
  done
  if [ -z "$local_port" ]; then
    echo "can't forward a port to $pod: $(cat "$work/pf.log")"
    kill "$pf" 2>/dev/null || true
    pf=""
    return 1
  fi
  code=$(curl -s -o "$work/body" -w '%{http_code}' --max-time 10 \
    --cacert "$work/ca.crt" --cert "$work/tls.crt" --key "$work/tls.key" \
    --resolve "$host:$local_port:127.0.0.1" "https://$host:$local_port/v1/event-gateways/$id/entities" || true)
  kill "$pf" 2>/dev/null || true
  wait "$pf" 2>/dev/null || true
  pf=""
  if [ "$code" != "$want" ]; then
    echo "$pod answered $code, not $want, for the gateway $id: $(cat "$work/body" 2>/dev/null)"
    return 1
  fi
  echo "$pod answered $code for the gateway $id"
done
}

for attempt in $(seq 1 "${ATTEMPTS:-30}"); do
  if out=$(check); then
    echo "$out"
    exit 0
  fi
  last="$out"
  sleep 2
done
echo "after ${ATTEMPTS:-30} attempts: $last" >&2
exit 1
