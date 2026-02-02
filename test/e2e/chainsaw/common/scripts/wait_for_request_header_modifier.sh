#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: Gateway proxy IP.
#   ROUTE_PATH: Route path prefix (e.g. "/httpbin-request-header-modifier").
# Optional:
#   ATTEMPTS: Number of attempts. Default: 60.
#   SLEEP_SECONDS: Sleep between attempts. Default: 5.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
ATTEMPTS="${ATTEMPTS:-60}"
SLEEP_SECONDS="${SLEEP_SECONDS:-5}"

out=""
for _ in $(seq 1 "${ATTEMPTS}"); do
  out="$(curl -s --header "X-Set:baz" --header "X-Remove:qux" "http://${PROXY_IP}${ROUTE_PATH}/headers" || true)"
  if printf '%s' "${out}" | grep -q '"X-Add"' &&
     printf '%s' "${out}" | grep -q '"X-Set": \\[' &&
     printf '%s' "${out}" | grep -q '"foo"' &&
     ! printf '%s' "${out}" | grep -q '"X-Remove"'; then
    echo "${out}"
    exit 0
  fi
  sleep "${SLEEP_SECONDS}"
done

echo "${out}"
exit 1

