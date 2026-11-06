#!/bin/bash
# Sourced by the helper scripts run through json_output.sh: what their commands
# need to print their result, and to have json_output.sh report the commands they
# ran.
#
# Variables (from environment):
#   JSON_OUTPUT_TRACE_DIR: Where json_output.sh reads the traced commands from.

# trace tool args...: runs the tool, recording its command line and, when it
# fails, its stderr, even for callers discarding it. Long arguments, like keys,
# are left out.
trace() {
  local tool=$1 rendered err exit_code=0 arg
  shift
  rendered=$tool
  for arg in "$@"; do
    if [ "${#arg}" -gt 200 ]; then
      rendered+=" <${#arg} bytes>"
    else
      rendered+=" $(printf '%q' "$arg")"
    fi
  done
  printf '%s' "$rendered" >"${JSON_OUTPUT_TRACE_DIR:?}/$tool.command"
  err=$(mktemp "$JSON_OUTPUT_TRACE_DIR/stderr.XXXXXX")
  command "$tool" "$@" 2>"$err" || exit_code=$?
  if [ "$exit_code" -ne 0 ]; then
    printf '%s' "$rendered" >"$JSON_OUTPUT_TRACE_DIR/$tool.failed_command"
    cp "$err" "$JSON_OUTPUT_TRACE_DIR/$tool.failed_output"
  fi
  cat "$err" >&2
  rm -f "$err"
  return "$exit_code"
}

kubectl() { trace kubectl "$@"; }
openssl() { trace openssl "$@"; }

# result key value...: prints the result of the command, a JSON object of strings.
result() {
  local args=()
  while [ $# -gt 0 ]; do
    args+=(--arg "$1" "$2")
    shift 2
  done
  jq -n "${args[@]}" '$ARGS.named'
}

# until_true timeout description command...: retries command every second,
# failing after timeout seconds. Only the failures of the last attempt are
# reported.
until_true() {
  local timeout=$1 what=$2 start=$SECONDS
  shift 2
  until
    rm -f "${JSON_OUTPUT_TRACE_DIR:?}"/*.failed_*
    "$@"
  do
    if ((SECONDS - start >= timeout)); then
      echo "timed out after ${timeout}s waiting for: $what" >&2
      return 1
    fi
    sleep 1
  done
  UNTIL_CONDITION=$what
  UNTIL_WAITED=$((SECONDS - start))
}

# until_result [key value...]: prints the result of a command which last called
# until_true: the condition it waited for, how long, and the given fields.
until_result() {
  result "$@" condition "$UNTIL_CONDITION" | jq --argjson waited "$UNTIL_WAITED" '. + {waited_seconds: $waited}'
}
