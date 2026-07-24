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


# The `telnet` command to check TCP connectivity to the proxy.
build_telnet_cmd() {
  local CMD="telnet ${PROXY_IP} ${PROXY_PORT}"
  # sleep for a second to receive the initial welcome message from the echo server.
  echo "sleep 1 | $CMD"
}

TELNET_CMD=$(build_telnet_cmd)

# Retry loop: Keep trying until we get the correct repsonse or run out of retries.
LAST_OUTPUT=""
for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  if OUTPUT=$(eval $TELNET_CMD 2>&1 | tr '\n' ' '); then
    LAST_OUTPUT="$OUTPUT"
    # Check if the output contains the welcome message from the echo pod.
    # Only "Running on Pod" is required.
    if [[ $OUTPUT =~ "Running on Pod" ]]; then
      # Success! Got the welcome message.
      cat <<EOF
{
  "success": true,
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "telnet_command": "$TELNET_CMD"
}
EOF
      exit 0
    fi

    # Got a response but not expected, retry.
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  else
    # nc command failed.
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
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "output": "$LAST_OUTPUT",
  "telnet_command": "$TELNET_CMD"
}
EOF
exit 1