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

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
HEADER_NAME="${HEADER_NAME}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# Retry 30 times with 2s delay (60s total)
for i in $(seq 1 30); do
  # Make HEAD request to get only headers
  CURL_CMD="curl -s -I"
  if [[ -n "$HOST" ]]; then
    CURL_CMD="$CURL_CMD -H 'Host: $HOST'"
  fi
  CURL_CMD="$CURL_CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"
  RESPONSE=$(eval $CURL_CMD 2>&1 || echo "")

  # Check HTTP status code
  STATUS_CODE=$(echo "$RESPONSE" | head -n 1 | grep -oE 'HTTP/[0-9.]+ ([0-9]+)' | grep -oE '[0-9]+$' || echo "")
  if [[ "$STATUS_CODE" != "200" ]]; then
    continue
  fi

  # Check if the header is present (case-insensitive)
  HEADER=$(echo "$RESPONSE" | grep -i "^${HEADER_NAME}:" | tr -d '\r' || echo "")

  # If header is absent, success
  if [[ -z "$HEADER" ]]; then
    cat <<EOF
{
  "success": true,
  "expected_header_name": "$HEADER_NAME",
  "attempts": $i,
  "message": "Header '$HEADER_NAME' correctly absent on attempt $i",
  "curl_command": "$CURL_CMD"
}
EOF
    exit 0
  fi

  # Sleep before next attempt (except on last attempt)
  if [ $i -lt 30 ]; then
    sleep 2
  fi
done

# All retries exhausted - header is still present
# Escape the response for JSON
ESCAPED_RESPONSE=$(echo "$RESPONSE" | jq -Rs . || echo "\"\"")
cat <<EOF
{
  "success": false,
  "expected_header_name": "$HEADER_NAME",
  "found_header": "$HEADER",
  "last_status_code": "$STATUS_CODE",
  "last_response": $ESCAPED_RESPONSE,
  "attempts": 30,
  "error": "Header '$HEADER_NAME' is still present after 30 attempts (60s total)",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "curl_command": "$CURL_CMD"
}
EOF
exit 1
