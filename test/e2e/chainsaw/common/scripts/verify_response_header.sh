#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Sends HEAD requests to the proxy until a 200 response carries the expected
# response header (EXPECTED_HEADER) or lacks the unwanted one (ABSENT_HEADER).
# Exactly one of EXPECTED_HEADER and ABSENT_HEADER must be set.
#
# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test.
#   EXPECTED_HEADER: (optional) The full header line that must be present
#     (e.g., "X-Add: foo" or "X-RateLimit-Limit-Minute: 20").
#   ABSENT_HEADER: (optional) The header name that must NOT be present (e.g., "X-Cleanup").
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
#   HOST: (optional) The Host header to send with the request.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
# Bracket PROXY_IP for use in a host:port string when it's an IPv6 address
# (identified by containing a colon), matching RFC 3986.
case "$PROXY_IP" in
  *:*) PROXY_HOST="[${PROXY_IP}]" ;;
  *) PROXY_HOST="$PROXY_IP" ;;
esac
ROUTE_PATH="${ROUTE_PATH}"
EXPECTED_HEADER="${EXPECTED_HEADER:-}"
ABSENT_HEADER="${ABSENT_HEADER:-}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

if [[ -n "$EXPECTED_HEADER" && -z "$ABSENT_HEADER" ]]; then
  # Extract header name from EXPECTED_HEADER (e.g., "X-Add" from "X-Add: foo").
  HEADER_NAME=$(echo "$EXPECTED_HEADER" | cut -d':' -f1)
elif [[ -z "$EXPECTED_HEADER" && -n "$ABSENT_HEADER" ]]; then
  HEADER_NAME="$ABSENT_HEADER"
else
  jq -n '{success: false, error: "exactly one of EXPECTED_HEADER and ABSENT_HEADER must be set"}'
  exit 1
fi

CURL_ARGS=(-s -I)
if [[ -n "$HOST" ]]; then
  CURL_ARGS+=(-H "Host: $HOST")
fi
URL="http://${PROXY_HOST}:${PROXY_PORT}${ROUTE_PATH}"
CURL_CMD="curl ${CURL_ARGS[*]} '${URL}'"

# Returns success when the header line satisfies the expectation.
header_matches() {
  local HEADER="$1"
  if [[ -n "$EXPECTED_HEADER" ]]; then
    [[ "$HEADER" == "$EXPECTED_HEADER" ]]
  else
    [[ -z "$HEADER" ]]
  fi
}

# Retry loop.
LAST_RESPONSE=""
LAST_STATUS_CODE=""
LAST_HEADER=""

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  RESPONSE=$(curl "${CURL_ARGS[@]}" "$URL" 2>&1 || echo "")
  LAST_RESPONSE="$RESPONSE"

  # Check HTTP status code.
  STATUS_CODE=$(echo "$RESPONSE" | head -n 1 | grep -oE 'HTTP/[0-9.]+ ([0-9]+)' | grep -oE '[0-9]+$' || echo "")
  LAST_STATUS_CODE="$STATUS_CODE"

  if [[ "$STATUS_CODE" == "200" ]]; then
    # Extract the header (case-insensitive), removing carriage returns.
    HEADER=$(echo "$RESPONSE" | grep -i "^${HEADER_NAME}:" | tr -d '\r' || echo "")
    LAST_HEADER="$HEADER"

    if header_matches "$HEADER"; then
      jq -n \
        --arg expected_header "$EXPECTED_HEADER" \
        --arg absent_header "$ABSENT_HEADER" \
        --arg found_header "$HEADER" \
        --argjson retry_attempt "$ATTEMPT" \
        --argjson max_retries "$MAX_RETRIES" \
        --arg curl_command "$CURL_CMD" \
        '{success: true, expected_header: $expected_header, absent_header: $absent_header, found_header: $found_header, retry_attempt: $retry_attempt, max_retries: $max_retries, curl_command: $curl_command}'
      exit 0
    fi
  fi

  if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
    sleep "$RETRY_DELAY"
  fi
done

# All retries exhausted.
jq -n \
  --arg error "Header expectation not met after $MAX_RETRIES attempts" \
  --arg expected_header "$EXPECTED_HEADER" \
  --arg absent_header "$ABSENT_HEADER" \
  --arg last_header "$LAST_HEADER" \
  --arg last_status_code "$LAST_STATUS_CODE" \
  --arg last_response "$LAST_RESPONSE" \
  --argjson max_retries "$MAX_RETRIES" \
  --arg proxy_ip "$PROXY_IP" \
  --arg proxy_port "$PROXY_PORT" \
  --arg route_path "$ROUTE_PATH" \
  --arg curl_command "$CURL_CMD" \
  '{success: false, error: $error, expected_header: $expected_header, absent_header: $absent_header, last_header: $last_header, last_status_code: $last_status_code, last_response: $last_response, max_retries: $max_retries, proxy_ip: $proxy_ip, proxy_port: $proxy_port, route_path: $route_path, curl_command: $curl_command}'
exit 1
