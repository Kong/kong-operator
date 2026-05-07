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
# Capture body to temp file, output only HTTP code to stdout.
BODY_FILE=$(mktemp /tmp/curl_body.XXXXXX)
build_curl_cmd() {
  local CMD="curl -s -w '%{http_code}' -X $METHOD "
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi
  CMD="$CMD 'http://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}' -o $BODY_FILE"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Function to extract info from response body.
# Body format:
#   Welcome, you are connected to node <node>.
#   Running on Pod <pod-name>.
#   In namespace <namespace>.
#   With IP address <ip>.
extract_body_info() {
  local BODY="$1"
  local POD_NODE=$(echo "$BODY" | grep -o 'connected to node [^.]*' | sed 's/connected to node //' || echo "")
  local POD_NAME=$(echo "$BODY" | grep -o 'Running on Pod [^.]*' | sed 's/Running on Pod //' || echo "")
  local POD_NAMESPACE=$(echo "$BODY" | grep -o 'In namespace [^.]*' | sed 's/In namespace //' || echo "")
  local POD_IP=$(echo "$BODY" | grep -o 'IP address .*' | sed 's/IP address //' || echo "")
  echo "${POD_NODE}|${POD_NAME}|${POD_NAMESPACE}|${POD_IP}"
}

# Retry loop: Keep trying until we get 200 or run out of retries.
HTTP_CODE=""
LAST_OUTPUT=""
LAST_BODY_INFO=""

for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  # Clear body file.
  > $BODY_FILE

  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_OUTPUT="$OUTPUT"

    # Read response body.
    BODY=$(cat $BODY_FILE 2>/dev/null || echo "")
    if [[ -n "$BODY" ]]; then
      LAST_BODY_INFO=$(extract_body_info "$BODY")
    fi

    if [[ "$HTTP_CODE" == "200" ]]; then
      # Success! Got 200.
      POD_NODE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f1)
      POD_NAME=$(echo "$LAST_BODY_INFO" | cut -d'|' -f2)
      POD_NAMESPACE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f3)
      POD_IP=$(echo "$LAST_BODY_INFO" | cut -d'|' -f4)
      rm -f $BODY_FILE
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
  "pod_node": "$POD_NODE",
  "pod_name": "$POD_NAME",
  "pod_namespace": "$POD_NAMESPACE",
  "pod_ip": "$POD_IP",
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
rm -f $BODY_FILE
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
