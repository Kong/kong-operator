#!/bin/bash
# Run a helper script dispatching several commands and print its outcome as one
# JSON object on stdout, following the JSON output convention of the README.
# Helpers call it first thing, unless JSON_OUTPUT_INNER is set, and source
# json_output_lib.sh:
#
#   [ -n "${JSON_OUTPUT_INNER:-}" ] || exec bash .../json_output.sh "$0" "$@"
#
# On success, the command must print a JSON object, its result: this script adds
# "success", the helper command and the last kubectl command. Anything else is a
# failure. On failure, "success" is false, and the result has "error", the last
# line of stderr, the helper command,
# the failed kubectl and openssl commands with their output, and all of stderr,
# and this script exits with the exit code of the command.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Arguments:
#   $1: The helper script.
#   $2...: The command of the helper and its arguments.

SCRIPT="$1"
shift

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
export JSON_OUTPUT_TRACE_DIR="${TMP_DIR}/trace"
mkdir "${JSON_OUTPUT_TRACE_DIR}"

# To files, not captured: a process the helper leaves behind can't hold them up.
EXIT_CODE=0
JSON_OUTPUT_INNER=1 "${SCRIPT}" "$@" >"${TMP_DIR}/stdout" 2>"${TMP_DIR}/stderr" || EXIT_CODE=$?

# What to run from the test directory to reproduce it.
COMMAND="NAMESPACE=$(printf '%q' "${NAMESPACE:-}")$(printf ' %q' "${SCRIPT}" "$@")"

if [[ "${EXIT_CODE}" -eq 0 ]]; then
  if jq -se 'length == 1 and (.[0] | type == "object")' "${TMP_DIR}/stdout" >/dev/null 2>&1; then
    KUBECTL_COMMAND="$(cat "${JSON_OUTPUT_TRACE_DIR}/kubectl.command" 2>/dev/null || true)"
    jq --arg command "${COMMAND}" --arg kubectl_command "${KUBECTL_COMMAND}" \
      '{success: true, command: $command} + (if $kubectl_command == "" then {} else {kubectl_command: $kubectl_command} end) + .' \
      "${TMP_DIR}/stdout"
    exit 0
  fi
  EXIT_CODE=1
  echo "the command printed no JSON object" >>"${TMP_DIR}/stderr"
fi

DIAGNOSTICS='{}'
for TOOL in kubectl openssl; do
  if [[ -f "${JSON_OUTPUT_TRACE_DIR}/${TOOL}.failed_command" ]]; then
    DIAGNOSTICS="$(jq \
      --arg tool "${TOOL}" \
      --rawfile failed_command "${JSON_OUTPUT_TRACE_DIR}/${TOOL}.failed_command" \
      --rawfile failed_output "${JSON_OUTPUT_TRACE_DIR}/${TOOL}.failed_output" \
      '. + {($tool + "_command"): $failed_command, ("last_" + $tool + "_output"): $failed_output}' <<<"${DIAGNOSTICS}")"
  elif [[ "${TOOL}" == "kubectl" && -f "${JSON_OUTPUT_TRACE_DIR}/kubectl.command" ]]; then
    DIAGNOSTICS="$(jq --rawfile last_command "${JSON_OUTPUT_TRACE_DIR}/kubectl.command" \
      '. + {kubectl_command: $last_command}' <<<"${DIAGNOSTICS}")"
  fi
done

ERROR="$(grep -v '^[[:space:]]*$' "${TMP_DIR}/stderr" | tail -n 1 || true)"
jq -n \
  --arg error "${ERROR:-exited with code ${EXIT_CODE}}" \
  --arg command "${COMMAND}" \
  --argjson exit_code "${EXIT_CODE}" \
  --argjson diagnostics "${DIAGNOSTICS}" \
  --rawfile stdout "${TMP_DIR}/stdout" \
  --rawfile stderr "${TMP_DIR}/stderr" \
  '{success: false, error: $error, command: $command, exit_code: $exit_code} + $diagnostics + {stdout: $stdout, stderr: $stderr}'
exit "${EXIT_CODE}"
