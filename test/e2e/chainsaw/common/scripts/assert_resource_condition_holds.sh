#!/bin/bash
# Assert that a jq condition on a Kubernetes resource stays true for a whole
# time window, e.g. that an object never becomes Programmed. Complements
# chainsaw's assert, which only waits until a condition becomes true once.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewaycustompolicies.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.
#   JQ_CONDITION: jq condition evaluated against the resource JSON that must
#     hold on every check, e.g.
#     '(.status.id // "") == "" and ([.status.conditions[]? | select(.type == "Programmed" and .status == "True")] | length == 0)'.
#   WINDOW_SECONDS: (optional) How long to watch. Default: 75 (longer than the
#     default 1 minute Konnect sync period).
#   CHECK_INTERVAL: (optional) Seconds between checks. Default: 5.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
JQ_CONDITION="${JQ_CONDITION}"
WINDOW_SECONDS="${WINDOW_SECONDS:-75}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5}"

DEADLINE=$((SECONDS + WINDOW_SECONDS))
CHECKS=0
while (( SECONDS < DEADLINE )); do
  if ! OBJ="$(kubectl get "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" -o json 2>&1)"; then
    jq -n \
      --arg error "failed to get ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME}" \
      --arg kubectl_output "${OBJ}" \
      '{error: $error, kubectl_output: $kubectl_output}'
    exit 1
  fi
  if ! jq -e "${JQ_CONDITION}" <<<"${OBJ}" >/dev/null; then
    jq -n \
      --arg error "condition no longer holds on ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME}" \
      --arg condition "${JQ_CONDITION}" \
      --argjson checks "${CHECKS}" \
      --argjson status "$(jq -c '.status // null' <<<"${OBJ}")" \
      '{error: $error, condition: $condition, checks_passed: $checks, status: $status}'
    exit 1
  fi
  CHECKS=$((CHECKS + 1))
  sleep "${CHECK_INTERVAL}"
done
jq -n --argjson checks "${CHECKS}" '{success: true, checks: $checks}'
