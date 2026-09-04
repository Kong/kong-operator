#!/bin/bash
# Point-in-time check (a single kubectl get, deliberately not a retry loop)
# that no resource matching a label selector exists right now.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'secrets').
#   NAMESPACE: The namespace to search in.
#   LABEL_SELECTOR: Label selector that should match zero resources.

RESOURCE_TYPE="${RESOURCE_TYPE}"
NAMESPACE="${NAMESPACE}"
LABEL_SELECTOR="${LABEL_SELECTOR}"

KUBECTL_CMD="kubectl get $RESOURCE_TYPE -n $NAMESPACE -l $LABEL_SELECTOR -o json"

if ! KUBECTL_OUTPUT=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -l "$LABEL_SELECTOR" -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to list $RESOURCE_TYPE",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "label_selector": "$LABEL_SELECTOR",
  "kubectl_command": "$KUBECTL_CMD",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

COUNT=$(echo "$KUBECTL_OUTPUT" | jq '.items | length')
if [[ "$COUNT" -ne 0 ]]; then
  cat <<EOF
{
  "error": "expected zero matching resources, found $COUNT",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "label_selector": "$LABEL_SELECTOR",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
  exit 1
fi

cat <<EOF
{
  "count": 0,
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "label_selector": "$LABEL_SELECTOR"
}
EOF
