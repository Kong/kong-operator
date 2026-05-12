#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   PROXY_PORT:  The port to connect to.
#   SNI: The server name indicator (SNI) used in establishing the TLS connection.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
PROXY_PORT="${PROXY_PORT}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Build openssl command.
build_openssl_cmd() {
  local CMD="openssl s_client -connect ${PROXY_IP}:${PROXY_PORT} -servername ${SNI} -quiet -no_ign_eof"
  # sleep for a second to receive the initial welcome message from the echo server.
  echo "(sleep 1; echo 'Q') | $CMD"
}

OPENSSL_CMD=$(build_openssl_cmd)

# Retry loop: Keep trying until we get the correct repsonse or run out of retries.
LAST_OUTPUT=""
for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  if OUTPUT=$(eval $OPENSSL_CMD 2>&1 | tr '\n' ' '); then
    LAST_OUTPUT="$OUTPUT"
    # Check if the output contains the welcome message from the echo pod.
    # Only "Running on Pod" is required: with TLS Passthrough the backend itself
    # terminates TLS and also emits "Through TLS connection", but with TLS Terminate
    # at the gateway the backend speaks plain TCP and never emits that substring.
    if [[ $OUTPUT =~ "Running on Pod" ]]; then
      # Success! Got the welcome message.
      cat <<EOF
{
  "success": true,
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "sni": "$SNI",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "openssl_command": "$OPENSSL_CMD"
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
  "success": false,
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "sni": "$SNI",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "output": "$LAST_OUTPUT",
  "openssl_command": "$OPENSSL_CMD"
}
EOF
exit 1