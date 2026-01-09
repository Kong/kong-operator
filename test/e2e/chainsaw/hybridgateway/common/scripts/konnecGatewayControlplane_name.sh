#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for the KonnectGatewayControlplane resource.

NAMESPACE="${NAMESPACE}"

# Fetch values and store in shell variables.
if ! KUBECTL_OUTPUT=$(kubectl get konnectgatewaycontrolplane -n "$NAMESPACE" -l gateway-operator.konghq.com/managed-by=gateway -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get KonnectGatewayControlPlane resource",
  "namespace": "$NAMESPACE",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

CP_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r '.items[0].metadata.name // ""')

# Final validation: Ensure critical variables are not empty.
if [ -z "$CP_NAME" ]; then
  cat <<EOF
{
  "error": "No KonnectGatewayControlPlane found with required label",
  "namespace": "$NAMESPACE",
  "label": "gateway-operator.konghq.com/managed-by=gateway",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, labels: .metadata.labels}]')
}
EOF
  exit 1
fi

# Output the JSON block for Chainsaw to parse.
cat <<EOF
{
  "cp_name": "$CP_NAME"
}
EOF
