#!/bin/bash
# Abort on unbound variables and pipeline errors. Connection attempts are handled explicitly.
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   PROXY_PORT: The TCP port to connect to.
#   EXPECTED_RESPONSE: (optional) Substring expected in the TCP response. Default: "Running on Pod".
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: 180.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: 1.

PROXY_IP="${PROXY_IP}"
PROXY_PORT="${PROXY_PORT}"
EXPECTED_RESPONSE="${EXPECTED_RESPONSE:-Running on Pod}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

LAST_OUTPUT=""
LAST_STATUS=0

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  OUTPUT=$(timeout 5 bash -c 'exec 3<>/dev/tcp/$1/$2; timeout 2 cat <&3' _ "$PROXY_IP" "$PROXY_PORT" 2>&1)
  STATUS=$?

  LAST_OUTPUT="$OUTPUT"
  LAST_STATUS="$STATUS"

  if [[ "$OUTPUT" == *"$EXPECTED_RESPONSE"* ]]; then
    cat <<EOF
{
  "success": true,
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "expected_response": "$EXPECTED_RESPONSE",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES
}
EOF
    exit 0
  fi

  if [[ "$ATTEMPT" -lt "$MAX_RETRIES" ]]; then
    sleep "$RETRY_DELAY"
  fi
done

cat <<EOF
{
  "success": false,
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "expected_response": "$EXPECTED_RESPONSE",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "last_status": $LAST_STATUS,
  "output": $(printf '%s' "$LAST_OUTPUT" | jq -Rs .)
}
EOF
exit 1
