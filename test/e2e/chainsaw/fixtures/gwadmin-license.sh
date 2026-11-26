#!/usr/bin/env bash
# GWAdmin test license input, separate from legacy KONG_LICENSE_DATA.
#
# Usage: bash gwadmin-license.sh check|install
#   GW_ADMIN_LICENSE_FILE: Path to the original compact JWS enveloped license.
#   NAMESPACE: GWAdmin namespace (install only); it must already exist.
#   GW_ADMIN_LICENSE_REQUIRED: Require input during install (default: false).
#   MAX_RETRIES: Install attempts (default: 180).
#   RETRY_DELAY: Seconds between install attempts (default: 1).
# check always requires input and never contacts Kubernetes. install permits
# absent input locally: no license Secret is provisioned, so gw-admin, which
# requires a license, doesn't start, and only the tests that don't need it
# running can pass. GWAdmin CI requires input.
set +x
set -euo pipefail

fail() {
  jq -n --arg error "$1" '{error: $error}'
  exit 1
}

case "${1:-}" in
  check|install) ;;
  *) fail "expected action 'check' or 'install'" ;;
esac

case "${GW_ADMIN_LICENSE_REQUIRED:-false}" in
  true|false) ;;
  *) fail "GW_ADMIN_LICENSE_REQUIRED must be 'true' or 'false'" ;;
esac

if [[ -z "${GW_ADMIN_LICENSE_FILE:-}" ]]; then
  if [[ "$1" == check || "${GW_ADMIN_LICENSE_REQUIRED:-false}" == true ]]; then
    fail "GW_ADMIN_LICENSE_FILE is required: supply a readable file containing an enveloped JWS license with event_gateway entitlement (not KONG_LICENSE_DATA JSON)"
  fi
  echo '{"license_input":"not-configured","message":"no license Secret provisioned, gw-admin will not start (not licensed CI)"}'
  exit 0
fi

[[ -f "$GW_ADMIN_LICENSE_FILE" && -r "$GW_ADMIN_LICENSE_FILE" && -s "$GW_ADMIN_LICENSE_FILE" ]] ||
  fail "GW_ADMIN_LICENSE_FILE must name a readable, non-empty regular file"

# Check only the envelope's shape. gw-admin owns signature, expiration and
# entitlement validation. Never decode, transform or print the supplied license.
license="$(cat -- "$GW_ADMIN_LICENSE_FILE" 2>/dev/null)" ||
  fail "could not read GW_ADMIN_LICENSE_FILE"
[[ "$license" =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]] ||
  fail "GW_ADMIN_LICENSE_FILE must contain one compact JWS enveloped license, not legacy Kong Enterprise JSON"
unset license

if [[ "$1" == check ]]; then
  echo '{"license_input":"checked"}'
  exit 0
fi

[[ -n "${NAMESPACE:-}" ]] || fail "NAMESPACE must be the namespace of the GWAdmin"

# Server-side apply avoids a last-applied annotation containing the license.
# kubectl errors can include the submitted object: suppress those payloads and
# surface a fixed error instead. The Secret never passes through Chainsaw's
# apply/assert renderer, JSON output wrapper, or uploaded debug files.
# Retried: the konghq.com/secret label routes the Secret through Kong Operator's
# validating webhook (failurePolicy: Fail), which can be unreachable for a
# moment after Kong Operator turns Ready.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
for i in $(seq 1 "${MAX_RETRIES}"); do
  if kubectl create secret generic gw-admin-license --namespace="$NAMESPACE" \
      --from-file="license=$GW_ADMIN_LICENSE_FILE" --dry-run=client -o json 2>/dev/null |
      jq '.metadata.labels["konghq.com/secret"] = "true"' 2>/dev/null |
      kubectl apply --server-side --field-manager=gwadmin-license-prereq -f - >/dev/null 2>&1; then
    jq -n --argjson i "$i" --argjson max "$MAX_RETRIES" \
      '{license_input: "installed", retry_attempt: $i, max_retries: $max}'
    exit 0
  fi
  if [[ $i -lt $MAX_RETRIES ]]; then
    sleep "${RETRY_DELAY}"
  fi
done
jq -n --argjson max "$MAX_RETRIES" \
  --arg error "could not provision gw-admin-license Secret after ${MAX_RETRIES} attempts; check namespace, Kubernetes access and server-side apply permissions/field ownership (payload-bearing kubectl output suppressed)" \
  '{error: $error, retry_attempt: $max, max_retries: $max}'
exit 1
