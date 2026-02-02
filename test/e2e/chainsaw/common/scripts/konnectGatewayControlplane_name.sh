#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   GATEWAY_NAME: The name of the gateway.
#   GATEWAY_NAMESPACE: The namespace of the gateway.

GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"

# Fetch values and store in shell variables.
if ! KUBECTL_OUTPUT=$(kubectl get konnectgatewaycontrolplane -n "$GATEWAY_NAMESPACE" -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get KonnectGatewayControlPlane resource",
  "namespace": "$GATEWAY_NAMESPACE",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# Find the ControlPlane that has the specified Gateway as owner
CP_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw_name "$GATEWAY_NAME" '
  .items[] | 
  select(
    .metadata.ownerReferences[]? | 
    .kind == "Gateway" and .name == $gw_name
  ) | .metadata.name' | head -n 1)

# Final validation: Ensure critical variables are not empty.
if [ -z "$CP_NAME" ]; then
  cat <<EOF
{
  "error": "No KonnectGatewayControlPlane found with Gateway owner reference",
  "namespace": "$GATEWAY_NAMESPACE",
  "gateway_name": "$GATEWAY_NAME",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, ownerReferences: .metadata.ownerReferences}]')
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
