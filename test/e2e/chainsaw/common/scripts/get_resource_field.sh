#!/bin/bash
# Read a single field of a Kubernetes resource and print it as
# {"value": <field value>} (null when the field is unset), e.g. to record a
# resource's status.id in a chainsaw script output. Retries until the field is
# set unless ALLOW_UNSET is "true".
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewaycustompolicies.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.
#   FIELD: jq path of the field to read (e.g. '.status.id').
#   ALLOW_UNSET: (optional) "true" to print {"value": null} right away when the
#     field is unset instead of retrying. Default: false.
#   MAX_RETRIES: (optional) Number of attempts. Default: 60.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
FIELD="${FIELD}"
ALLOW_UNSET="${ALLOW_UNSET:-false}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-1}"

KUBECTL_OUTPUT=""
for _ in $(seq 1 "${MAX_RETRIES}"); do
  if ! KUBECTL_OUTPUT="$(kubectl get "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" -o json 2>&1)"; then
    sleep "${RETRY_DELAY}"
    continue
  fi
  VALUE="$(jq -c "${FIELD}" <<<"${KUBECTL_OUTPUT}")"
  if [[ "${VALUE}" != "null" || "${ALLOW_UNSET}" == "true" ]]; then
    jq -n --argjson value "${VALUE}" '{value: $value}'
    exit 0
  fi
  sleep "${RETRY_DELAY}"
done

jq -n \
  --arg error "field ${FIELD} of ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME} is not set" \
  --arg kubectl_output "${KUBECTL_OUTPUT}" \
  '{error: $error, kubectl_output: $kubectl_output}'
exit 1
