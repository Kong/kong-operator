#!/bin/bash
# Common helper functions for chainsaw test scripts.
#
# Usage: source this file at the beginning of your script:
#   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#   source "${SCRIPT_DIR}/_helpers.sh"

# retry_until_success - Retry a command until it succeeds or max attempts reached.
#
# Why not use curl's --retry?
#   curl's --retry only handles transient errors (connection failures, timeouts, etc.).
#   It cannot retry based on response content. In our case, even when the request
#   returns 200 OK, the headers might not yet be modified by the Kong plugin
#   (e.g., during plugin configuration propagation). We need to check the actual
#   response content and retry until the expected headers are present.
#
# Arguments:
#   $1 - Max number of attempts (default: 60)
#   $2 - Sleep seconds between attempts (default: 5)
#   $3... - Command to execute (should return 0 on success, non-zero on failure)
#
# Returns:
#   0 if the command eventually succeeds, 1 if all attempts exhausted.
#
# Example:
#   retry_until_success 30 2 check_my_condition
retry_until_success() {
  local max_attempts="${1:-60}"
  local sleep_seconds="${2:-5}"
  shift 2

  for _ in $(seq 1 "${max_attempts}"); do
    if "$@"; then
      return 0
    fi
    sleep "${sleep_seconds}"
  done
  return 1
}

# check_header_exists - Check if a header exists in the response.
#
# Arguments:
#   $1 - Response content
#   $2 - Header pattern to search for (grep pattern)
#
# Returns:
#   0 if header exists, 1 otherwise.
check_header_exists() {
  local response="$1"
  local pattern="$2"
  printf '%s' "${response}" | grep -q "${pattern}"
}

# check_header_not_exists - Check if a header does NOT exist in the response.
#
# Arguments:
#   $1 - Response content
#   $2 - Header pattern to search for (grep pattern)
#
# Returns:
#   0 if header does NOT exist, 1 if it exists.
check_header_not_exists() {
  local response="$1"
  local pattern="$2"
  ! printf '%s' "${response}" | grep -q "${pattern}"
}
