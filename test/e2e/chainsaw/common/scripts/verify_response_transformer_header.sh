#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test.
#   EXPECTED_HEADER: The expected header value (e.g., "X-Add: foo").
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
#   HOST: (optional) The Host header to send with the request.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
EXPECTED_HEADER="${EXPECTED_HEADER}"
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

  # Extract the custom header, removing carriage returns
  # Extract header name from EXPECTED_HEADER (e.g., "X-Add" from "X-Add: foo")
  HEADER_NAME=$(echo "$EXPECTED_HEADER" | cut -d':' -f1)
  HEADER=$(echo "$RESPONSE" | grep -i "^${HEADER_NAME}:" | tr -d '\r' || echo "")

  # Check if header matches expected value
  if [[ "$HEADER" == "$EXPECTED_HEADER" ]]; then
    cat <<EOF
{
  "success": true,
  "expected_header": "$EXPECTED_HEADER",
  "found_header": "$HEADER",
  "attempts": $i,
  "message": "Response transformer header matched on attempt $i",
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

# All retries exhausted
# Escape the response for JSON
ESCAPED_RESPONSE=$(echo "$RESPONSE" | jq -Rs . || echo "\"\"")
cat <<EOF
{
  "success": false,
  "expected_header": "$EXPECTED_HEADER",
  "last_header": "$HEADER",
  "last_status_code": "$STATUS_CODE",
  "last_response": $ESCAPED_RESPONSE,
  "attempts": 30,
  "error": "Header did not match expected value after 30 attempts (60s total)",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "curl_command": "$CURL_CMD"
}
EOF
exit 1
