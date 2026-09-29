#!/bin/bash
# Apply a single JSON patch operation to one field of a Kubernetes resource
# (optionally of its status subresource), then print the field's resulting
# value, read from the patch response, as {"value": <field value>,
# "retry_attempt": <n>} (value is null when the field is unset), e.g. to clear
# a resource's status.id. Retries while the patch fails, e.g. because the
# resource or the field to remove does not exist yet, or the API server
# returned a transient error.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewaycustompolicies.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.
#   FIELD_PATH: JSON pointer of the field to patch (e.g. '/status/id').
#   OP: (optional) JSON patch operation: 'remove', 'replace' or 'add'. Default: remove.
#   VALUE: (required for 'replace' and 'add') The new value, as JSON (e.g. '"abc"').
#   SUBRESOURCE: (optional) Subresource to patch (e.g. 'status'). Default: none.
#   MAX_RETRIES: (optional) Number of attempts. Default: 30.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
FIELD_PATH="${FIELD_PATH}"
OP="${OP:-remove}"
VALUE="${VALUE:-}"
SUBRESOURCE="${SUBRESOURCE:-}"
MAX_RETRIES="${MAX_RETRIES:-30}"
RETRY_DELAY="${RETRY_DELAY:-1}"

fail() {
  jq -n --arg error "$1" --arg details "${2:-}" '{error: $error, details: $details}'
  exit 1
}

case "${OP}" in
  remove)
    PATCH="$(jq -cn --arg path "${FIELD_PATH}" '[{op: "remove", path: $path}]')"
    ;;
  replace|add)
    [[ -n "${VALUE}" ]] || fail "VALUE is required for OP=${OP}"
    PATCH="$(jq -cn --arg op "${OP}" --arg path "${FIELD_PATH}" --argjson value "${VALUE}" \
      '[{op: $op, path: $path, value: $value}]')" || fail "VALUE is not valid JSON" "${VALUE}"
    ;;
  *)
    fail "unknown OP '${OP}'"
    ;;
esac

SUBRESOURCE_ARGS=()
if [[ -n "${SUBRESOURCE}" ]]; then
  SUBRESOURCE_ARGS=(--subresource="${SUBRESOURCE}")
fi

# Convert the JSON pointer (/a/b) into a jq path (.a.b) to read the result back.
JQ_PATH="$(jq -rn --arg p "${FIELD_PATH}" \
  '$p | ltrimstr("/") | split("/") | map(gsub("~1"; "/") | gsub("~0"; "~") | "[" + tojson + "]") | "." + join("")')"

ERR_FILE="$(mktemp)"
trap 'rm -f "${ERR_FILE}"' EXIT

PATCH_OUTPUT=""
for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  # Read the result from the patch response rather than a follow-up get, so a
  # controller changing the field right after the patch cannot race the check.
  if PATCH_OUTPUT="$(kubectl patch "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" \
    ${SUBRESOURCE_ARGS[@]+"${SUBRESOURCE_ARGS[@]}"} --type=json -p "${PATCH}" -o json 2>"${ERR_FILE}")"; then
    jq -c --argjson attempt "${ATTEMPT}" "{value: ${JQ_PATH}, retry_attempt: \$attempt}" <<<"${PATCH_OUTPUT}"
    exit 0
  fi
  if [[ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]]; then
    sleep "${RETRY_DELAY}"
  fi
done
fail "patching ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME} failed after ${MAX_RETRIES} attempts" "$(cat "${ERR_FILE}")"
