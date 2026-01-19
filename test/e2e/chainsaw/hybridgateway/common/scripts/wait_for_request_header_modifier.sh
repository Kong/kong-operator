#!/bin/bash
# Wait for request header modifier plugin to take effect.
#
# This script polls the gateway until the request header modifications are
# correctly applied. It verifies that:
#   - X-Add header is added
#   - X-Set header is set to the expected value (foo)
#   - X-Remove header is removed
#
# Why custom retry instead of curl's --retry?
#   curl's --retry only handles transient errors (connection failures, timeouts).
#   During plugin configuration propagation, the request may return 200 OK but
#   headers are not yet modified. We need content-based retry logic.
#
# Required environment variables:
#   PROXY_IP: Gateway proxy IP address.
#   ROUTE_PATH: Route path prefix (e.g., "/httpbin-request-header-modifier").
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

# check_request_headers - Verify request header modifications.
#
# Sends a request with X-Set and X-Remove headers, then checks that:
#   - X-Add header was added by the plugin
#   - X-Set header was modified to contain "foo"
#   - X-Remove header was removed by the plugin
check_request_headers() {
  LAST_RESPONSE="$(curl -s \
    --header "X-Set:baz" \
    --header "X-Remove:qux" \
    "http://${PROXY_IP}${ROUTE_PATH}/headers" || true)"

  check_header_exists "${LAST_RESPONSE}" '"X-Add"' &&
    check_header_exists "${LAST_RESPONSE}" '"X-Set": \[' &&
    check_header_exists "${LAST_RESPONSE}" '"foo"' &&
    check_header_not_exists "${LAST_RESPONSE}" '"X-Remove"'
}

if retry_until_success "${ATTEMPTS}" "${SLEEP_SECONDS}" check_request_headers; then
  echo "${LAST_RESPONSE}"
  exit 0
else
  echo "Timed out waiting for request header modifier to take effect" >&2
  echo "${LAST_RESPONSE}"
  exit 1
fi
