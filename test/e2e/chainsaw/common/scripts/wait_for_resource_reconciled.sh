#!/bin/bash
# Wait until a resource's current generation has been reconciled, i.e. until
# the observedGeneration of its CONDITION_TYPE condition equals its
# metadata.generation, and the resource satisfies a jq condition. Changing the
# resource's spec (which bumps its generation) and calling this script proves
# that a reconciliation ran after the change, without waiting for a periodic
# resync. The condition is retried until it holds (a transient reconciliation
# error does not fail it) and fails only at the timeout.
#
# Prints {"generation": <n>, "status": <resource status>} on success.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewayconsumers.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.
#   CONDITION_TYPE: (optional) Condition whose observedGeneration to wait for.
#     Default: Programmed.
#   JQ_CONDITION: (optional) jq condition the reconciled resource must
#     satisfy, e.g. '(.status.id // "") == ""'. Default: true.
#   MAX_RETRIES: (optional) Number of attempts. Default: 120.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
CONDITION_TYPE="${CONDITION_TYPE:-Programmed}"
JQ_CONDITION="${JQ_CONDITION:-true}"
MAX_RETRIES="${MAX_RETRIES:-120}"
RETRY_DELAY="${RETRY_DELAY:-1}"

KUBECTL_OUTPUT=""
RECONCILED=false
for _ in $(seq 1 "${MAX_RETRIES}"); do
  if KUBECTL_OUTPUT="$(kubectl get "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" -o json 2>&1)" &&
    jq -e --arg type "${CONDITION_TYPE}" '
      .metadata.generation as $gen
      | [.status.conditions[]? | select(.type == $type and .observedGeneration == $gen)] | length > 0
    ' <<<"${KUBECTL_OUTPUT}" >/dev/null; then
    RECONCILED=true
    if jq -e "${JQ_CONDITION}" <<<"${KUBECTL_OUTPUT}" >/dev/null; then
      jq -c '{generation: .metadata.generation, status}' <<<"${KUBECTL_OUTPUT}"
      exit 0
    fi
  fi
  sleep "${RETRY_DELAY}"
done

if [[ "${RECONCILED}" == "true" ]]; then
  ERROR="${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME} was reconciled but does not satisfy: ${JQ_CONDITION}"
else
  ERROR="generation of ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME} was not reconciled (${CONDITION_TYPE} condition)"
fi
jq -n \
  --arg error "${ERROR}" \
  --arg kubectl_output "${KUBECTL_OUTPUT}" \
  '{error: $error, last_kubectl_output: $kubectl_output}'
exit 1
