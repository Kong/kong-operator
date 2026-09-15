#!/bin/bash
# Point-in-time check that a resource exists right now (a single kubectl get,
# deliberately not a retry loop) -- used where the test needs to catch a
# resource having already been removed too early, not wait for it to show up.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewaydataplanecertificates').
#   RESOURCE_NAME: The name of the resource to check.
#   NAMESPACE: The namespace of the resource.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"

KUBECTL_CMD="kubectl get $RESOURCE_TYPE $RESOURCE_NAME -n $NAMESPACE"

if ! kubectl get "$RESOURCE_TYPE" "$RESOURCE_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
  cat <<EOF
{
  "exists": false,
  "error": "resource not found",
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
  exit 1
fi

cat <<EOF
{
  "exists": true,
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE"
}
EOF
