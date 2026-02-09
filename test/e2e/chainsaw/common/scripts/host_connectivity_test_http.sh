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
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Build curl command with optional Host header and HTTP method.
build_curl_cmd() {
  local CMD="curl -s -o /dev/null -w '%{http_code}' -X $METHOD "
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi
  CMD="$CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Retry loop: Keep trying until we get 200 or run out of retries.
HTTP_CODE=""
LAST_OUTPUT=""
for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_OUTPUT="$OUTPUT"

    if [[ "$HTTP_CODE" == "200" ]]; then
      # Success! Got 200.
      cat <<EOF
{
  "http_status": $HTTP_CODE,
  "success": true,
  "message": "HTTP $METHOD request to $PROXY_IP:$PROXY_PORT$ROUTE_PATH successful",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "curl_command": "$CURL_CMD"
}
EOF
      exit 0
    fi

    # Got a response but not 200, retry.
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  else
    # Curl command failed.
    LAST_OUTPUT="$OUTPUT"
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  fi
done

# All retries exhausted, output failure.
cat <<EOF
{
  "http_status": ${HTTP_CODE:-"000"},
  "success": false,
  "error": "Request failed with status ${HTTP_CODE:-unknown} after $MAX_RETRIES attempts",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .)
}
EOF
exit 1
