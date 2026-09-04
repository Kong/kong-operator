#!/bin/bash
# Point-in-time read of a pod-template annotation (deliberately not a retry loop) --
# only safe to call once the Deployment is already known to exist, e.g. right after
# a preceding assert confirmed Ready=True.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace of the Deployment.
#   DEPLOYMENT_NAME: The name of the Deployment.
#   ANNOTATION_KEY: The pod-template annotation key to read (spec.template.metadata.annotations).

NAMESPACE="${NAMESPACE}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME}"
ANNOTATION_KEY="${ANNOTATION_KEY}"

KUBECTL_CMD="kubectl get deployment $DEPLOYMENT_NAME -n $NAMESPACE -o json"

if ! KUBECTL_OUTPUT=$(kubectl get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get Deployment resource",
  "deployment_name": "$DEPLOYMENT_NAME",
  "namespace": "$NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

VALUE=$(echo "$KUBECTL_OUTPUT" | jq -r --arg key "$ANNOTATION_KEY" '.spec.template.metadata.annotations[$key] // ""')

cat <<EOF
{
  "value": "$VALUE",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
