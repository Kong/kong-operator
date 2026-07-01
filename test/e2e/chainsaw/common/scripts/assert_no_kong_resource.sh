#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The full resource type, e.g. kongcertificates.configuration.konghq.com.
#   GATEWAY_NAME: The name of the gateway.
#   GATEWAY_NAMESPACE: The namespace of the gateway.
#   NAMESPACE: The namespace to search for the resource.
#   HTTP_ROUTE_NAME: (Optional) The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: (Optional) The namespace of the HTTPRoute.

RESOURCE_TYPE="${RESOURCE_TYPE}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
NAMESPACE="${NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME:-}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE:-}"

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT=""
if [[ -n "$HTTP_ROUTE_NAME" && -n "$HTTP_ROUTE_NAMESPACE" ]]; then
  EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"
fi

# Fetch all resources
KUBECTL_CMD="kubectl get ${RESOURCE_TYPE} -n ${NAMESPACE} -o json"

if ! KUBECTL_OUTPUT=$(kubectl get "${RESOURCE_TYPE}" -n "${NAMESPACE}" -o json 2>&1); then
  # Error executing kubectl - resource type might not exist or other issue
  cat <<EOF
{
  "status": "query_failed",
  "resource_type": "${RESOURCE_TYPE}",
  "namespace": "${NAMESPACE}",
  "gateway": "${EXPECTED_GW}",
  "kubectl_command": "${KUBECTL_CMD}",
  "kubectl_output": $(echo "${KUBECTL_OUTPUT}" | jq -Rs .)
}
EOF
  exit 0
fi

# Find resources that match annotations
# If route is specified, require both gateway and route annotations
# If route is not specified, only require gateway annotation
if [[ -n "$EXPECTED_RT" ]]; then
  MATCHED_RESOURCES=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw "$EXPECTED_GW" --arg rt "$EXPECTED_RT" '
    [.items[] |
    select(
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])) and
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt]))
    ) | {name: .metadata.name, kind: .kind, namespace: .metadata.namespace}]')
else
  MATCHED_RESOURCES=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw "$EXPECTED_GW" '
    [.items[] |
    select(
      .metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])
    ) | {name: .metadata.name, kind: .kind, namespace: .metadata.namespace}]')
fi

# Check if any matching resources were found
RESOURCE_COUNT=$(echo "$MATCHED_RESOURCES" | jq 'length')

if [[ "$RESOURCE_COUNT" -gt 0 ]]; then
  # Resources exist - this is unexpected
  FILTER_DESC="gateway=${EXPECTED_GW}"
  if [[ -n "$EXPECTED_RT" ]]; then
    FILTER_DESC="${FILTER_DESC}, route=${EXPECTED_RT}"
  fi

  cat <<EOF
{
  "error": "Unexpected ${RESOURCE_TYPE} resources found matching ${FILTER_DESC}",
  "resource_type": "${RESOURCE_TYPE}",
  "namespace": "${NAMESPACE}",
  "gateway": "${EXPECTED_GW}",
  "route": "${EXPECTED_RT}",
  "found_count": ${RESOURCE_COUNT},
  "found_resources": ${MATCHED_RESOURCES},
  "kubectl_command": "${KUBECTL_CMD}"
}
EOF
  exit 1
fi

# No matching resources found - success
cat <<EOF
{
  "status": "success",
  "message": "No ${RESOURCE_TYPE} resources found",
  "resource_type": "${RESOURCE_TYPE}",
  "namespace": "${NAMESPACE}",
  "gateway": "${EXPECTED_GW}",
  "route": "${EXPECTED_RT}"
}
EOF
