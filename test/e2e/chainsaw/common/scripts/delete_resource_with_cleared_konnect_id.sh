#!/bin/bash
# Clear a Konnect-managed resource's status.id and delete the resource at the
# same time, as if the operator had lost the Konnect ID just before the
# resource was deleted. Does not wait for the deletion to complete.
#
# The two requests race on purpose, and the outcome decides which operator
# path runs: if the operator handles both changes in one reconciliation, it
# sees a resource being deleted without an ID and finds its Konnect entity by
# the k8s-uid label; if it reconciles the cleared status.id first, it recovers
# the ID (create conflict + k8s-uid lookup) and then deletes by ID. Callers
# can only rely on the end result (the Konnect entity is deleted), not on the
# path taken.
#
# Prints {"deleted": "<type>/<namespace>/<name>"}.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewayconsumers.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"

# Run the status patch concurrently with the deletion. The patch may fail if
# the deletion already completed, which is fine.
kubectl patch "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" \
  --subresource=status --type=json -p '[{"op":"remove","path":"/status/id"}]' >/dev/null 2>&1 &
PATCH_PID=$!

if ! DELETE_OUTPUT="$(kubectl delete "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" --wait=false 2>&1)"; then
  wait "${PATCH_PID}" || true
  jq -n \
    --arg error "failed to delete ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME}" \
    --arg kubectl_output "${DELETE_OUTPUT}" \
    '{error: $error, kubectl_output: $kubectl_output}'
  exit 1
fi
wait "${PATCH_PID}" || true

jq -n --arg deleted "${RESOURCE_TYPE}/${NAMESPACE}/${RESOURCE_NAME}" '{deleted: $deleted}'
