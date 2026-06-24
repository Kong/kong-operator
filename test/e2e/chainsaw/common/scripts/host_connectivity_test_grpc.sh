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
#   CALL_TIMEOUT: (optional) Per-RPC overall timeout in seconds (grpcurl -max-time). Default: '5'.
#   CONNECT_TIMEOUT: (optional) Connect-only timeout in seconds (grpcurl -connect-timeout).
#                    Default: min(3, CALL_TIMEOUT). Reserves budget so connect can't drain the whole CALL_TIMEOUT.
#   GRPCURL_BIN: (optional) Path to the grpcurl binary. Default: resolve from PATH.
#   GRPCBIN_PROTO: (optional) Path to a grpcbin proto file used by grpcurl when server reflection is unavailable.
#                  Default: ../proto/grpcbin.proto relative to this script.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v jq >/dev/null 2>&1; then
  printf '{"success":false,"error":"jq binary not found in PATH"}\n'
  exit 1
fi

normalize_bool() {
  local key="$1"
  local value="$2"

  case "$value" in
    1|t|T|true|TRUE|True)
      printf 'true\n'
      ;;
    0|f|F|false|FALSE|False)
      printf 'false\n'
      ;;
    *)
      printf '%s must be a boolean: %s\n' "$key" "$value" >&2
      return 1
      ;;
  esac
}

require_non_negative_int() {
  local key="$1"
  local value="$2"

  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    printf '%s must be a non-negative integer: %s\n' "$key" "$value" >&2
    return 1
  fi
}

require_positive_int() {
  local key="$1"
  local value="$2"

  require_non_negative_int "$key" "$value" || return 1
  if [[ "$value" -le 0 ]]; then
    printf '%s must be greater than zero: %s\n' "$key" "$value" >&2
    return 1
  fi
}

int_or_zero() {
  local value="$1"
  if [[ "$value" =~ ^[0-9]+$ ]]; then
    printf '%s' "$value"
  else
    printf '0'
  fi
}

bool_or_false() {
  if [[ "${1:-}" == "true" ]]; then
    printf 'true'
  else
    printf 'false'
  fi
}

write_output() {
  local success="$1"
  local retry_attempt="$2"
  local response="$3"
  local error="$4"

  local proxy_port_num retry_attempt_num max_retries_num use_tls_bool insecure_tls_bool success_bool
  proxy_port_num=$(int_or_zero "${PROXY_PORT:-0}")
  retry_attempt_num=$(int_or_zero "$retry_attempt")
  max_retries_num=$(int_or_zero "${MAX_RETRIES:-0}")
  use_tls_bool=$(bool_or_false "${USE_TLS:-false}")
  insecure_tls_bool=$(bool_or_false "${INSECURE_TLS:-false}")
  success_bool=$(bool_or_false "$success")

  jq -n \
    --argjson success "$success_bool" \
    --arg address "$ADDRESS" \
    --arg host "$GRPC_HOST" \
    --arg proxy_ip "$PROXY_IP" \
    --argjson proxy_port "$proxy_port_num" \
    --arg message "$REQUEST_MESSAGE" \
    --arg response "$response" \
    --arg error "$error" \
    --argjson retry_attempt "$retry_attempt_num" \
    --argjson max_retries "$max_retries_num" \
    --argjson use_tls "$use_tls_bool" \
    --argjson insecure_tls "$insecure_tls_bool" \
    '{
      success: $success,
      address: $address,
      host: $host,
      proxy_ip: $proxy_ip,
      proxy_port: $proxy_port,
      message: $message,
      retry_attempt: $retry_attempt,
      max_retries: $max_retries,
      use_tls: $use_tls,
      insecure_tls: $insecure_tls
    }
    + (if $response != "" then {response: $response} else {} end)
    + (if $error != "" then {error: $error} else {} end)'
}

fail_with_json() {
  local error="$1"
  local retry_attempt="${2:-0}"

  write_output false "$retry_attempt" "" "$error"
  exit 1
}

resolve_grpcurl_bin() {
  if [[ -n "${GRPCURL_BIN:-}" ]]; then
    if [[ -x "${GRPCURL_BIN}" ]]; then
      printf '%s\n' "${GRPCURL_BIN}"
      return 0
    fi

    printf 'GRPCURL_BIN is not executable: %s\n' "${GRPCURL_BIN}" >&2
    return 1
  fi

  local resolved
  if resolved=$(command -v grpcurl 2>/dev/null); then
    printf '%s\n' "$resolved"
    return 0
  fi

  printf 'grpcurl binary not found in PATH and GRPCURL_BIN is not set\n' >&2
  return 1
}

resolve_grpcbin_proto() {
  local proto_path="${GRPCBIN_PROTO:-${SCRIPT_DIR}/../proto/grpcbin.proto}"

  if [[ -f "$proto_path" ]]; then
    local proto_dir proto_name
    proto_dir="$(CDPATH= cd -- "$(dirname -- "$proto_path")" && pwd)"
    proto_name="$(basename -- "$proto_path")"
    printf '%s/%s\n' "$proto_dir" "$proto_name"
    return 0
  fi

  printf 'grpcbin proto file not found: %s\n' "$proto_path" >&2
  return 1
}

combine_output() {
  local stderr_output="$1"
  local stdout_output="$2"

  if [[ -n "$stderr_output" && -n "$stdout_output" ]]; then
    printf '%s\n%s\n' "$stderr_output" "$stdout_output"
  elif [[ -n "$stderr_output" ]]; then
    printf '%s\n' "$stderr_output"
  else
    printf '%s\n' "$stdout_output"
  fi
}

PROXY_IP="${PROXY_IP:-}"
GRPC_HOST="${GRPC_HOST:-}"
PROXY_PORT="${PROXY_PORT:-443}"
REQUEST_MESSAGE="${REQUEST_MESSAGE:-kong}"
USE_TLS_RAW="${USE_TLS:-true}"
INSECURE_TLS_RAW="${INSECURE_TLS:-true}"
USE_TLS="false"
INSECURE_TLS="false"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
CALL_TIMEOUT="${CALL_TIMEOUT:-5}"
if [[ -n "${CONNECT_TIMEOUT:-}" ]]; then
  : # honor caller override
elif [[ "$CALL_TIMEOUT" =~ ^[0-9]+$ ]] && (( CALL_TIMEOUT < 3 )); then
  CONNECT_TIMEOUT="$CALL_TIMEOUT"
else
  CONNECT_TIMEOUT="3"
fi
ADDRESS="${PROXY_IP}:${PROXY_PORT}"

if [[ -z "$PROXY_IP" ]]; then
  fail_with_json "PROXY_IP is required"
fi

if [[ -z "$GRPC_HOST" ]]; then
  fail_with_json "GRPC_HOST is required"
fi

require_positive_int "PROXY_PORT" "$PROXY_PORT" || fail_with_json "PROXY_PORT must be a positive integer: ${PROXY_PORT}"
require_positive_int "MAX_RETRIES" "$MAX_RETRIES" || fail_with_json "MAX_RETRIES must be a positive integer: ${MAX_RETRIES}"
require_non_negative_int "RETRY_DELAY" "$RETRY_DELAY" || fail_with_json "RETRY_DELAY must be a non-negative integer: ${RETRY_DELAY}"
require_positive_int "CALL_TIMEOUT" "$CALL_TIMEOUT" || fail_with_json "CALL_TIMEOUT must be a positive integer: ${CALL_TIMEOUT}"
require_positive_int "CONNECT_TIMEOUT" "$CONNECT_TIMEOUT" || fail_with_json "CONNECT_TIMEOUT must be a positive integer: ${CONNECT_TIMEOUT}"

if ! USE_TLS=$(normalize_bool "USE_TLS" "$USE_TLS_RAW"); then
  fail_with_json "USE_TLS must be a boolean: ${USE_TLS_RAW}"
fi

if ! INSECURE_TLS=$(normalize_bool "INSECURE_TLS" "$INSECURE_TLS_RAW"); then
  fail_with_json "INSECURE_TLS must be a boolean: ${INSECURE_TLS_RAW}"
fi

if ! GRPCURL_BIN=$(resolve_grpcurl_bin); then
  fail_with_json "Failed to resolve grpcurl binary"
fi

if ! GRPCBIN_PROTO=$(resolve_grpcbin_proto); then
  fail_with_json "Failed to resolve grpcbin proto file"
fi

GRPCBIN_IMPORT_PATH="$(dirname -- "$GRPCBIN_PROTO")"
GRPCBIN_PROTO_NAME="$(basename -- "$GRPCBIN_PROTO")"

if ! REQUEST_BODY=$(jq -cn --arg message "$REQUEST_MESSAGE" '{fString: $message}'); then
  fail_with_json "Failed to build grpcurl request payload"
fi

declare -a GRPCURL_CMD=(
  "$GRPCURL_BIN"
  "-authority" "$GRPC_HOST"
  "-connect-timeout" "$CONNECT_TIMEOUT"
  "-max-time" "$CALL_TIMEOUT"
  "-format-error"
  "-import-path" "$GRPCBIN_IMPORT_PATH"
  "-proto" "$GRPCBIN_PROTO_NAME"
)

if [[ "$USE_TLS" == "true" ]]; then
  if [[ "$INSECURE_TLS" == "true" ]]; then
    GRPCURL_CMD+=("-insecure")
  fi
else
  GRPCURL_CMD+=("-plaintext")
fi

GRPCURL_CMD+=(
  "-d" "$REQUEST_BODY"
  "$ADDRESS"
  "grpcbin.GRPCBin/DummyUnary"
)

STDERR_FILE="$(mktemp)"
trap 'rm -f "$STDERR_FILE"' EXIT

LAST_ERROR=""

for (( attempt = 1; attempt <= MAX_RETRIES; attempt++ )); do
  if OUTPUT=$("${GRPCURL_CMD[@]}" 2>"${STDERR_FILE}"); then
    if ! RESPONSE=$(printf '%s' "$OUTPUT" | jq -er '.fString // .f_string // empty' 2>/dev/null); then
      LAST_ERROR="missing fString in grpcurl response: ${OUTPUT}"
    elif [[ "$RESPONSE" != "$REQUEST_MESSAGE" ]]; then
      LAST_ERROR="unexpected response from GRPC server: ${RESPONSE}"
    else
      write_output true "$attempt" "$RESPONSE" ""
      exit 0
    fi
  else
    STDERR_OUTPUT="$(cat "${STDERR_FILE}")"
    LAST_ERROR="$(combine_output "$STDERR_OUTPUT" "$OUTPUT")"
  fi

  if (( attempt < MAX_RETRIES )); then
    sleep "$RETRY_DELAY"
  fi
done

fail_with_json "$LAST_ERROR" "$MAX_RETRIES"
