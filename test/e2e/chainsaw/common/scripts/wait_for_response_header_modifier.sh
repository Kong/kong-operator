#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: Gateway proxy IP.
#   ROUTE_PATH: Route path prefix (e.g. "/httpbin-response-header-modifier").
# Optional:
#   ATTEMPTS: Number of attempts. Default: 60.
#   SLEEP_SECONDS: Sleep between attempts. Default: 5.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
ATTEMPTS="${ATTEMPTS:-60}"
SLEEP_SECONDS="${SLEEP_SECONDS:-5}"

out=""
for _ in $(seq 1 "${ATTEMPTS}"); do
  out="$(curl -sI --connect-timeout 5 --max-time 10 "http://${PROXY_IP}${ROUTE_PATH}/response-headers?x-remove=qux&x-set=baz" || true)"
  if printf '%s' "${out}" | grep -q '200 OK' &&
     printf '%s' "${out}" | grep -q 'X-Add: bar' &&
     printf '%s' "${out}" | grep -q 'X-Set: foo' &&
     ! printf '%s' "${out}" | grep -q 'X-Remove'; then
    echo "${out}"
    exit 0
  fi
  sleep "${SLEEP_SECONDS}"
done

echo "${out}"
exit 1

