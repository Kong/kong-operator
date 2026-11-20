#!/bin/bash
# Abort on unbound variables and pipeline errors. Connection attempts are handled explicitly.
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROTOCOL: The L4 protocol to use: 'tcp' or 'udp'.
#   PROXY_IP: The IP address of the proxy to connect to.
#   PROXY_PORT: The port to connect to.
#   EXPECTED_RESPONSE: (optional) Substring expected in the response. Default: "Running on Pod".
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: 180 for tcp, 30 for udp.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: 1 for tcp, 2 for udp.

PROTOCOL="${PROTOCOL}"
PROXY_IP="${PROXY_IP}"
PROXY_PORT="${PROXY_PORT}"
EXPECTED_RESPONSE="${EXPECTED_RESPONSE:-Running on Pod}"

# run_with_timeout SECONDS COMMAND [ARG...]: a portable replacement for GNU
# coreutils' timeout(1), which macOS does not ship by default. COMMAND runs in
# the background and is killed after SECONDS; 124 is returned on timeout, like
# timeout(1).
run_with_timeout() {
  local timeout=$1
  local pid
  local waited=0
  shift

  "$@" &
  pid=$!

  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$timeout" ]; then
      kill -TERM "$pid" 2>/dev/null
      # Don't wait forever if the command ignores SIGTERM.
      sleep 1
      kill -KILL "$pid" 2>/dev/null
      wait "$pid" 2>/dev/null
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done

  wait "$pid"
}

case "$PROTOCOL" in
  tcp)
    MAX_RETRIES="${MAX_RETRIES:-180}"
    RETRY_DELAY="${RETRY_DELAY:-1}"
    # The echo server greets as soon as the connection is established. Read
    # its lines until the read timeout so the probe does not wait for the
    # server to close. read -t still assigns a final partial line when it
    # times out, so print that line after the loop too.
    PROBE='exec 3<>/dev/tcp/$1/$2 || exit 1; while IFS= read -r -t 2 -u 3 line; do printf "%s\n" "$line"; done; [ -z "$line" ] || printf "%s\n" "$line"'
    ;;
  udp)
    MAX_RETRIES="${MAX_RETRIES:-30}"
    RETRY_DELAY="${RETRY_DELAY:-2}"
    # UDP is connectionless: send a datagram to get a reply. dd returns after
    # reading the reply datagram, while the outer timeout bounds a missing one.
    PROBE='exec 3<>/dev/udp/$1/$2 || exit 1; printf "probe" >&3; exec dd bs=4096 count=1 <&3 2>/dev/null'
    ;;
  *)
    echo "{\"success\": false, \"error\": \"unsupported PROTOCOL '$PROTOCOL', expected 'tcp' or 'udp'\"}"
    exit 1
    ;;
esac

LAST_OUTPUT=""
LAST_STATUS=0

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  OUTPUT=$(run_with_timeout 5 bash -c "$PROBE" _ "$PROXY_IP" "$PROXY_PORT" 2>&1)
  STATUS=$?

  LAST_OUTPUT="$OUTPUT"
  LAST_STATUS="$STATUS"

  if [[ "$OUTPUT" == *"$EXPECTED_RESPONSE"* ]]; then
    cat <<EOF
{
  "success": true,
  "protocol": "$PROTOCOL",
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
  "protocol": "$PROTOCOL",
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
