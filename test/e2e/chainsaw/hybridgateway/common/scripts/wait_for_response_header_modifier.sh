#!/bin/bash
# Wait for response header modifier plugin to take effect.
#
# This script polls the gateway until the response header modifications are
# correctly applied. It verifies that:
#   - Response status is 200 OK
#   - X-Add header is added with value "bar"
#   - X-Set header is set to "foo"
#   - X-Remove header is removed
#
# Why custom retry instead of curl's --retry?
#   curl's --retry only handles transient errors (connection failures, timeouts).
#   During plugin configuration propagation, the request may return 200 OK but
#   headers are not yet modified. We need content-based retry logic.
#
# Required environment variables:
#   PROXY_IP: Gateway proxy IP address.
#   ROUTE_PATH: Route path prefix (e.g., "/httpbin-response-header-modifier").
#
# Optional environment variables:
#   ATTEMPTS: Number of retry attempts. Default: 60.
#   SLEEP_SECONDS: Seconds to sleep between attempts. Default: 5.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/_helpers.sh"

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
ATTEMPTS="${ATTEMPTS:-60}"
SLEEP_SECONDS="${SLEEP_SECONDS:-5}"

# Global variable to store the last response for final output.
LAST_RESPONSE=""

# check_response_headers - Verify response header modifications.
#
# Sends a request and checks that the response headers are correctly modified:
#   - Status is 200 OK
#   - X-Add header is present with value "bar"
#   - X-Set header is set to "foo"
#   - X-Remove header is not present
check_response_headers() {
  LAST_RESPONSE="$(curl -sI \
    --connect-timeout 5 \
    --max-time 10 \
    "http://${PROXY_IP}${ROUTE_PATH}/response-headers?x-remove=qux&x-set=baz" || true)"

  check_header_exists "${LAST_RESPONSE}" '200 OK' &&
    check_header_exists "${LAST_RESPONSE}" 'X-Add: bar' &&
    check_header_exists "${LAST_RESPONSE}" 'X-Set: foo' &&
    check_header_not_exists "${LAST_RESPONSE}" 'X-Remove'
}

if retry_until_success "${ATTEMPTS}" "${SLEEP_SECONDS}" check_response_headers; then
  echo "${LAST_RESPONSE}"
  exit 0
else
  echo "Timed out waiting for response header modifier to take effect" >&2
  echo "${LAST_RESPONSE}"
  exit 1
fi
