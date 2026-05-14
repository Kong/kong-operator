#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: (optional) Resource kind to inspect. One of: 'gateway' (default), 'dataplane'.
#   RESOURCE_NAME: The name of the resource. Falls back to GATEWAY_NAME for backward compatibility.
#   RESOURCE_NAMESPACE: The namespace of the resource. Falls back to GATEWAY_NAMESPACE for backward compatibility.
#   GATEWAY_NAME / GATEWAY_NAMESPACE: Deprecated aliases for RESOURCE_NAME / RESOURCE_NAMESPACE,
#     preserved so existing callers (RESOURCE_TYPE=gateway) continue to work unchanged.
#
# The status path is the same for both kinds: .status.addresses[0].value.

RESOURCE_TYPE="${RESOURCE_TYPE:-gateway}"
RESOURCE_NAME="${RESOURCE_NAME:-${GATEWAY_NAME:-}}"
RESOURCE_NAMESPACE="${RESOURCE_NAMESPACE:-${GATEWAY_NAMESPACE:-}}"

case "$RESOURCE_TYPE" in
  gateway)
    KIND_ARG="gateway"
    ;;
  dataplane)
    KIND_ARG="dataplane.gateway-operator.konghq.com"
    ;;
  *)
    cat <<EOF
{
  "error": "Unsupported RESOURCE_TYPE",
  "resource_type": "$RESOURCE_TYPE",
  "supported_resource_types": ["gateway", "dataplane"]
}
EOF
    exit 1
    ;;
esac

KUBECTL_CMD="kubectl get ${KIND_ARG} ${RESOURCE_NAME} -n ${RESOURCE_NAMESPACE} -o json"

# Capture kubectl output for debugging.
if ! KUBECTL_OUTPUT=$(kubectl get "${KIND_ARG}" "${RESOURCE_NAME}" -n "${RESOURCE_NAMESPACE}" -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get ${RESOURCE_TYPE} resource",
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "resource_namespace": "$RESOURCE_NAMESPACE",
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
  "error": "No proxy IP address found in ${RESOURCE_TYPE} status",
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "resource_namespace": "$RESOURCE_NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD",
  "resource_status": $(echo "$KUBECTL_OUTPUT" | jq -c '.status // {}')
}
EOF
  exit 1
fi

cat <<EOF
{
  "proxy_ip_address": "$PROXY_IP_ADDRESS",
  "resource_type": "$RESOURCE_TYPE",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
