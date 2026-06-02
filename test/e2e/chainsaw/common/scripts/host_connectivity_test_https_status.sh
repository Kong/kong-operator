#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Generic HTTPS status code matcher. Performs an HTTPS request against the proxy and
# retries until the response status matches EXPECTED_STATUS_REGEX or retries are exhausted.
# Use for both success (e.g. ^200$) and failure (e.g. ^5..$) verifications.
#
# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   EXPECTED_STATUS_REGEX: Bash-compatible regex matched against the HTTP status code (e.g. '^200$', '^5..$').
#   PROXY_PORT: (optional) The port to connect to. Default: '443'.
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.
#   METHOD: (optional) The HTTP method to use. Default: 'GET'.
#   HOST: (optional) The Host header to send with the request.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '60'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '2'.

PROXY_IP="${PROXY_IP}"
EXPECTED_STATUS_REGEX="${EXPECTED_STATUS_REGEX}"
PROXY_PORT="${PROXY_PORT:-443}"
ROUTE_PATH="${ROUTE_PATH:-/}"
METHOD="${METHOD:-GET}"
HOST="${HOST:-}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-2}"

BODY_FILE=$(mktemp /tmp/curl_https_body.XXXXXX)
trap 'rm -f "$BODY_FILE"' EXIT

build_curl_cmd() {
  local CMD="curl -sk -w '%{http_code}' -X $METHOD "
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi
  CMD="$CMD 'https://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}' -o $BODY_FILE"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

HTTP_CODE=""
LAST_OUTPUT=""

for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  > $BODY_FILE

  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_OUTPUT="$OUTPUT"

    if [[ "$HTTP_CODE" =~ $EXPECTED_STATUS_REGEX ]]; then
      cat <<EOF
{
  "http_status": "$HTTP_CODE",
  "success": true,
  "expected_status_regex": $(echo "$EXPECTED_STATUS_REGEX" | jq -Rs .),
  "message": "HTTPS $METHOD request to $PROXY_IP:$PROXY_PORT$ROUTE_PATH matched expected status",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .)
}
EOF
      exit 0
    fi

    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  else
    LAST_OUTPUT="$OUTPUT"
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  fi
done

# Truncate response body to first 500 chars for the failure payload.
RESPONSE_BODY=$(cat "$BODY_FILE" 2>/dev/null | head -c 500 || echo "")

cat <<EOF
{
  "http_status": "${HTTP_CODE:-000}",
  "success": false,
  "expected_status_regex": $(echo "$EXPECTED_STATUS_REGEX" | jq -Rs .),
  "error": "Status ${HTTP_CODE:-unknown} did not match expected regex after $MAX_RETRIES attempts",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "method": "$METHOD",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .),
  "response_body": $(echo "$RESPONSE_BODY" | jq -Rs .)
}
EOF
exit 1
