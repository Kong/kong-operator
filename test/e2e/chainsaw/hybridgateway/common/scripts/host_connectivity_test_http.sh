#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   METHOD: The HTTP method to use (e.g., 'GET', 'POST', 'PUT').
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
#   HOST: (optional) The Host header to send with the request.

PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# Build curl command with optional Host header and HTTP method
CURL_CMD="curl --fail --retry 10 --retry-delay 5 --retry-all-errors -s -o /dev/null -w '%{http_code}' -X $METHOD "
if [[ -n "$HOST" ]]; then
  CURL_CMD="$CURL_CMD -H 'Host: $HOST' "
fi
CURL_CMD="$CURL_CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}' -v"

# Capture curl output, and handle failures gracefully
if ! OUTPUT=$(eval $CURL_CMD 2>&1); then
  # Curl failed - output the full debug info
  cat <<EOF
{
  "error": "Curl command failed",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# The last line of the output is the HTTP code from -w
HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)

# Validation for Chainsaw 'check' block or manual exit
if [[ "$HTTP_CODE" != "200" ]]; then
  cat <<EOF
{
  "http_status": $HTTP_CODE,
  "error": "Request failed with status $HTTP_CODE",
  "method": "$METHOD",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# Output JSON result
cat <<EOF
{
  "http_status": $HTTP_CODE,
  "success": true,
  "message": "HTTP $METHOD request to $PROXY_IP:$PROXY_PORT$ROUTE_PATH successful",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD"
}
EOF
