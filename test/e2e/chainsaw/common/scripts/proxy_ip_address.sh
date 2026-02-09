#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   GATEWAY_NAME: The name of the Gateway resource.
#   GATEWAY_NAMESPACE: The namespace of the Gateway resource.

GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"

KUBECTL_CMD="kubectl get gateway $GATEWAY_NAME -n $GATEWAY_NAMESPACE -o json"

# Capture kubectl output for debugging
if ! KUBECTL_OUTPUT=$(kubectl get gateway ${GATEWAY_NAME} -n ${GATEWAY_NAMESPACE} -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get gateway resource",
  "gateway_name": "$GATEWAY_NAME",
  "gateway_namespace": "$GATEWAY_NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

PROXY_IP_ADDRESS=$(echo "$KUBECTL_OUTPUT" | jq -r '.status.addresses[0].value // ""')

if [[ -z "$PROXY_IP_ADDRESS" || "$PROXY_IP_ADDRESS" == "null" ]]; then
  cat <<EOF
{
  "error": "No proxy IP address found in gateway status",
  "gateway_name": "$GATEWAY_NAME",
  "gateway_namespace": "$GATEWAY_NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD",
  "gateway_status": $(echo "$KUBECTL_OUTPUT" | jq -c '.status // {}')
}
EOF
  exit 1
fi

cat <<EOF
{
  "proxy_ip_address": "$PROXY_IP_ADDRESS",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
