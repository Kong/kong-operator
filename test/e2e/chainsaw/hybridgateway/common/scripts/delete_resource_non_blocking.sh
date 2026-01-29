#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The type of resource to delete (e.g., 'secret', 'configmap', 'konnectapiauthconfiguration').
#   RESOURCE_NAME: The name of the resource to delete.
#   RESOURCE_NAMESPACE: The namespace of the resource to delete.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
RESOURCE_NAMESPACE="${RESOURCE_NAMESPACE}"

RESOURCE_ID="${RESOURCE_TYPE}/${RESOURCE_NAME}"
NAMESPACE_ARG=""
if [[ -n "$RESOURCE_NAMESPACE" && "$RESOURCE_NAMESPACE" != "null" ]]; then
  NAMESPACE_ARG="-n $RESOURCE_NAMESPACE"
  RESOURCE_ID="$RESOURCE_ID -n $RESOURCE_NAMESPACE"
fi

DELETE_COMMAND="kubectl delete $RESOURCE_TYPE $RESOURCE_NAME $NAMESPACE_ARG --wait=false"

# Execute the command, it will return immediately due to --wait=false
$DELETE_COMMAND

cat <<EOF
{
  "command": "$DELETE_COMMAND",
  "status": "initiated",
  "message": "Resource deletion initiated (non-blocking)",
  "resource": "$RESOURCE_ID"
}
EOF
