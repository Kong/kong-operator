#!/bin/bash
# Send a UDP packet to a target host:port and assert that the response
# contains an expected substring. Used to verify which backend a Kong UDP
# listener is forwarding to (e.g. winner vs loser of GEP-2645 arbitration).
#
# Required env:
#   NAMESPACE     Namespace in which to spawn the probe Pod.
#   PROXY_IP      IP / hostname of the Kong proxy.
#   PROXY_PORT    UDP port of the listener under test.
#   EXPECTED_POD  Substring expected in the UDP response (go-echo emits
#                 "Running on Pod <POD_NAME>.").
# Optional env:
#   PROBE_IMAGE   Image to run the probe in. Default: busybox:1.36.
#   TIMEOUT_SECS  nc -w timeout. Default: 5.
#   MAX_RETRIES   Retry attempts (helps absorb propagation delay between Kong
#                 picking up a new config and traffic actually flowing).
#                 Default: 30.
#   RETRY_DELAY   Seconds between retries. Default: 2.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
PROXY_IP="${PROXY_IP}"
PROXY_PORT="${PROXY_PORT}"
EXPECTED_POD="${EXPECTED_POD}"
PROBE_IMAGE="${PROBE_IMAGE:-busybox:1.36}"
TIMEOUT_SECS="${TIMEOUT_SECS:-5}"
MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_DELAY="${RETRY_DELAY:-2}"

LAST_RESP=""
for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  POD="udp-probe-$(date +%s%N)"
  RESP=$(kubectl run "${POD}" \
    --image="${PROBE_IMAGE}" \
    --rm -i --restart=Never \
    -n "${NAMESPACE}" \
    --command -- sh -c "echo -n probe | nc -u -w${TIMEOUT_SECS} ${PROXY_IP} ${PROXY_PORT}" 2>/dev/null \
    | grep -v '^pod "' || true)
  LAST_RESP="${RESP}"
  if echo "${RESP}" | grep -q "${EXPECTED_POD}"; then
    echo "OK: attempt ${ATTEMPT}/${MAX_RETRIES} got expected pod '${EXPECTED_POD}' in response: ${RESP}"
    exit 0
  fi
  if [[ ${ATTEMPT} -lt ${MAX_RETRIES} ]]; then
    sleep "${RETRY_DELAY}"
  fi
done

echo "FAIL: expected pod '${EXPECTED_POD}' not in response after ${MAX_RETRIES} attempts. Last response: '${LAST_RESP}'" >&2
exit 1
