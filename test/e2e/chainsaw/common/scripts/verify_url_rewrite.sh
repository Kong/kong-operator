#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies that the gateway rewrote the request path before proxying it upstream.
#
# The upstream is go-httpbin: its /anything endpoint echoes the received request as JSON,
# including the final "url" the backend saw. The check succeeds when that "url" ends with
# EXPECTED_URL_SUFFIX, e.g. a request to /get/a rewritten by a URLRewrite filter with
# replacePrefixMatch "/anything/" must produce "url" ending with "/anything/a".
#
# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test (e.g. '/get/a').
#   EXPECTED_URL_SUFFIX: The suffix the echoed "url" must end with (e.g. '/anything/a').
#   HOST: (optional) The Host header to send with the request.
#   METHOD: (optional) The HTTP method to use. Default: 'GET'.
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
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
EXPECTED_URL_SUFFIX="${EXPECTED_URL_SUFFIX}"
HOST="${HOST:-}"
METHOD="${METHOD:-GET}"
PROXY_PORT="${PROXY_PORT:-80}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

BODY_FILE=$(mktemp /tmp/curl_body.XXXXXX)
build_curl_cmd() {
  local CMD="curl -s -w '%{http_code}' -X $METHOD "
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi
  CMD="$CMD 'http://${PROXY_HOST}:${PROXY_PORT}${ROUTE_PATH}' -o $BODY_FILE"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# The echoed "url" must be present and end with the expected rewritten suffix.
url_matches() {
  local URL="$1"
  [[ -n "$URL" && "$URL" == *"$EXPECTED_URL_SUFFIX" ]]
}

LAST_URL=""
LAST_BODY=""
LAST_STATUS_CODE=""

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  > "$BODY_FILE"

  if OUTPUT=$(eval "$CURL_CMD" 2>&1); then
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_STATUS_CODE="$HTTP_CODE"
    LAST_BODY=$(cat "$BODY_FILE" 2>/dev/null || echo "")

    if [[ "$HTTP_CODE" == "200" && -n "$LAST_BODY" ]]; then
      LAST_URL=$(echo "$LAST_BODY" | jq -r '.url // ""')

      if url_matches "$LAST_URL"; then
        rm -f "$BODY_FILE"
        jq -n \
          --arg url "$LAST_URL" \
          --arg expected_url_suffix "$EXPECTED_URL_SUFFIX" \
          --arg route_path "$ROUTE_PATH" \
          --arg host_header "$HOST" \
          --arg method "$METHOD" \
          --arg proxy_ip "$PROXY_IP" \
          --arg proxy_port "$PROXY_PORT" \
          --arg curl_command "$CURL_CMD" \
          --argjson retry_attempt "$ATTEMPT" \
          --argjson max_retries "$MAX_RETRIES" \
          '{success: true, url: $url, expected_url_suffix: $expected_url_suffix, route_path: $route_path, host_header: $host_header, method: $method, proxy_ip: $proxy_ip, proxy_port: $proxy_port, curl_command: $curl_command, retry_attempt: $retry_attempt, max_retries: $max_retries}'
        exit 0
      fi
    fi
  fi

  if [[ $ATTEMPT -lt "$MAX_RETRIES" ]]; then
    sleep "$RETRY_DELAY"
  fi
done

# All retries exhausted.
rm -f "$BODY_FILE"
jq -n \
  --arg error "Rewritten URL expectation not met after $MAX_RETRIES attempts" \
  --arg expected_url_suffix "$EXPECTED_URL_SUFFIX" \
  --arg last_url "$LAST_URL" \
  --arg last_status_code "$LAST_STATUS_CODE" \
  --arg last_body "$LAST_BODY" \
  --arg route_path "$ROUTE_PATH" \
  --arg host_header "$HOST" \
  --arg method "$METHOD" \
  --arg proxy_ip "$PROXY_IP" \
  --arg proxy_port "$PROXY_PORT" \
  --arg max_retries "$MAX_RETRIES" \
  --arg curl_command "$CURL_CMD" \
  '{success: false, error: $error, expected_url_suffix: $expected_url_suffix, last_url: $last_url, last_status_code: $last_status_code, last_body: $last_body, route_path: $route_path, host_header: $host_header, method: $method, proxy_ip: $proxy_ip, proxy_port: $proxy_port, max_retries: $max_retries, curl_command: $curl_command}'
exit 1
