#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   GRPC_HOST: The :authority/SNI hostname to send with the gRPC request.
#   PROXY_PORT: (optional) The proxy port. Default: '443'.
#   REQUEST_MESSAGE: (optional) Payload sent to grpcbin DummyUnary. Default: 'kong'.
#   USE_TLS: (optional) Whether to use TLS. Default: 'true'.
#   INSECURE_TLS: (optional) Whether to skip TLS verification. Default: 'true'.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.
#   CALL_TIMEOUT: (optional) Per-RPC timeout in seconds. Default: '5'.
#   DIAL_TIMEOUT: (optional) Dial timeout in seconds. Default: '5'.

PROXY_IP="${PROXY_IP}"
GRPC_HOST="${GRPC_HOST}"
PROXY_PORT="${PROXY_PORT:-443}"
REQUEST_MESSAGE="${REQUEST_MESSAGE:-kong}"
USE_TLS="${USE_TLS:-true}"
INSECURE_TLS="${INSECURE_TLS:-true}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
CALL_TIMEOUT="${CALL_TIMEOUT:-5}"
DIAL_TIMEOUT="${DIAL_TIMEOUT:-5}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"
CLIENT_PKG="./test/e2e/chainsaw/common/scripts/grpc_echo_client"
GO_RUN_CMD="cd \"${REPO_ROOT}\" && PROXY_IP=\"${PROXY_IP}\" GRPC_HOST=\"${GRPC_HOST}\" PROXY_PORT=\"${PROXY_PORT}\" REQUEST_MESSAGE=\"${REQUEST_MESSAGE}\" USE_TLS=\"${USE_TLS}\" INSECURE_TLS=\"${INSECURE_TLS}\" MAX_RETRIES=\"${MAX_RETRIES}\" RETRY_DELAY=\"${RETRY_DELAY}\" CALL_TIMEOUT=\"${CALL_TIMEOUT}\" DIAL_TIMEOUT=\"${DIAL_TIMEOUT}\" go run ${CLIENT_PKG}"
STDERR_FILE="$(mktemp)"
trap 'rm -f "$STDERR_FILE"' EXIT

if OUTPUT=$(cd "${REPO_ROOT}" && \
  PROXY_IP="${PROXY_IP}" \
  GRPC_HOST="${GRPC_HOST}" \
  PROXY_PORT="${PROXY_PORT}" \
  REQUEST_MESSAGE="${REQUEST_MESSAGE}" \
  USE_TLS="${USE_TLS}" \
  INSECURE_TLS="${INSECURE_TLS}" \
  MAX_RETRIES="${MAX_RETRIES}" \
  RETRY_DELAY="${RETRY_DELAY}" \
  CALL_TIMEOUT="${CALL_TIMEOUT}" \
  DIAL_TIMEOUT="${DIAL_TIMEOUT}" \
  go run "${CLIENT_PKG}" 2>"${STDERR_FILE}"); then
  echo "$OUTPUT"
  if [[ -s "${STDERR_FILE}" ]]; then
    cat "${STDERR_FILE}" >&2
  fi
  exit 0
fi

STDERR_OUTPUT="$(cat "${STDERR_FILE}")"

if echo "$OUTPUT" | jq empty >/dev/null 2>&1; then
  echo "$OUTPUT"
  if [[ -n "$STDERR_OUTPUT" ]]; then
    printf '%s\n' "$STDERR_OUTPUT" >&2
  fi
  exit 1
fi

COMBINED_OUTPUT="$OUTPUT"
if [[ -n "$STDERR_OUTPUT" && -n "$COMBINED_OUTPUT" ]]; then
  COMBINED_OUTPUT="${STDERR_OUTPUT}"
  COMBINED_OUTPUT+=$'\n'
  COMBINED_OUTPUT+="$OUTPUT"
elif [[ -n "$STDERR_OUTPUT" ]]; then
  COMBINED_OUTPUT="$STDERR_OUTPUT"
fi

jq -n \
  --arg error "Failed to execute gRPC connectivity helper" \
  --arg proxy_ip "$PROXY_IP" \
  --arg grpc_host "$GRPC_HOST" \
  --arg proxy_port "$PROXY_PORT" \
  --arg go_run_command "$GO_RUN_CMD" \
  --arg output "$COMBINED_OUTPUT" \
  '{error: $error, proxy_ip: $proxy_ip, grpc_host: $grpc_host, proxy_port: $proxy_port, go_run_command: $go_run_command, output: $output}'
exit 1

