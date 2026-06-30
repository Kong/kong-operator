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
#   REQUEST_HEADERS: (optional) Custom headers to send, format: 'Header1:value1,Header2:value2'.
#   EXPECTED_STATUS: The expected HTTP status code (e.g., '200', '404').
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"
REQUEST_HEADERS="${REQUEST_HEADERS:-}"
EXPECTED_STATUS="${EXPECTED_STATUS}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Build curl command with optional Host header and HTTP method
build_curl_cmd() {
  local CMD="curl -s -v -w '\n%{http_code}' -X $METHOD "

  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi

  # Add custom request headers if provided
  if [[ -n "$REQUEST_HEADERS" ]]; then
    IFS=',' read -ra HEADERS <<< "$REQUEST_HEADERS"
    for HEADER in "${HEADERS[@]}"; do
      HEADER=$(echo "$HEADER" | xargs)
      if [[ -n "$HEADER" ]]; then
        CMD="$CMD -H '$HEADER' "
      fi
    done
  fi

  CMD="$CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Retry loop: Keep trying until we get the expected status or run out of retries.
HTTP_CODE=""
LAST_OUTPUT=""
for i in $(seq 1 $MAX_RETRIES); do
  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_OUTPUT="$OUTPUT"

    if [[ "$HTTP_CODE" == "$EXPECTED_STATUS" ]]; then
      # Success! Got the expected status.
      cat <<EOF
{
  "http_status": $HTTP_CODE,
  "expected_status": $EXPECTED_STATUS,
  "success": true,
  "message": "HTTP $METHOD request to $PROXY_IP:$PROXY_PORT$ROUTE_PATH returned expected status $HTTP_CODE",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "request_headers": "$REQUEST_HEADERS",
  "retry_attempt": $i,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .)
}
EOF
      exit 0
    fi

    # Got a response but not the expected status, retry.
    if [[ $i -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  else
    # Curl command failed.
    LAST_OUTPUT="$OUTPUT"
    if [[ $i -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  fi
done

# All retries exhausted, output failure.
cat <<EOF
{
  "http_status": ${HTTP_CODE:-"000"},
  "expected_status": $EXPECTED_STATUS,
  "error": "Request returned status ${HTTP_CODE:-unknown} but expected $EXPECTED_STATUS after $MAX_RETRIES attempts",
  "success": false,
  "method": "$METHOD",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "request_headers": "$REQUEST_HEADERS",
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .)
}
EOF
exit 1
