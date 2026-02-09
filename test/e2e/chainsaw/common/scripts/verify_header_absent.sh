#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test.
#   HEADER_NAME: The header name that should NOT be present (e.g., "X-Cleanup").
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
#   HOST: (optional) The Host header to send with the request.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
HEADER_NAME="${HEADER_NAME}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Build curl command.
build_curl_cmd() {
  local CMD="curl -s -I"
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST'"
  fi
  CMD="$CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Retry loop.
LAST_RESPONSE=""
LAST_STATUS_CODE=""
LAST_HEADER=""

for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  RESPONSE=$(eval $CURL_CMD 2>&1 || echo "")
  LAST_RESPONSE="$RESPONSE"

  # Check HTTP status code.
  STATUS_CODE=$(echo "$RESPONSE" | head -n 1 | grep -oE 'HTTP/[0-9.]+ ([0-9]+)' | grep -oE '[0-9]+$' || echo "")
  LAST_STATUS_CODE="$STATUS_CODE"

  if [[ "$STATUS_CODE" != "200" ]]; then
    # Not 200 yet, retry.
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
    continue
  fi

  # Check if the header is present (case-insensitive).
  HEADER=$(echo "$RESPONSE" | grep -i "^${HEADER_NAME}:" | tr -d '\r' || echo "")
  LAST_HEADER="$HEADER"

  # If header is absent, success.
  if [[ -z "$HEADER" ]]; then
    cat <<EOF
{
  "success": true,
  "expected_header_name": "$HEADER_NAME",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "message": "Header '$HEADER_NAME' correctly absent on attempt $ATTEMPT",
  "curl_command": "$CURL_CMD"
}
EOF
    exit 0
  fi

  # Header still present, retry.
  if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
    sleep $RETRY_DELAY
  fi
done

# All retries exhausted - header is still present.
# Escape the response for JSON.
ESCAPED_RESPONSE=$(echo "$LAST_RESPONSE" | jq -Rs . || echo "\"\"")
cat <<EOF
{
  "success": false,
  "expected_header_name": "$HEADER_NAME",
  "found_header": "$LAST_HEADER",
  "last_status_code": "$LAST_STATUS_CODE",
  "last_response": $ESCAPED_RESPONSE,
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "error": "Header '$HEADER_NAME' is still present after $MAX_RETRIES attempts",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "curl_command": "$CURL_CMD"
}
EOF
exit 1
