#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for the resource.
#   RESOURCE_TYPE: The full resource type, e.g. kongservices.configuration.konghq.com.
#   GATEWAY_NAME: The name of the gateway.
#   GATEWAY_NAMESPACE: The namespace of the gateway.
#   HTTP_ROUTE_NAME: The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: The namespace of the HTTPRoute.

NAMESPACE="${NAMESPACE}"
RESOURCE_TYPE="${RESOURCE_TYPE}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"

# Fetch all resources and filter by the specific annotation CSV logic.
if ! KUBECTL_OUTPUT=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get resource",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

RESOURCE_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw "$EXPECTED_GW" --arg rt "$EXPECTED_RT" '
  .items[] | 
  select(
    (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])) and
    (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt]))
  ) | .metadata.name' | head -n 1)

if [[ -z "$RESOURCE_NAME" || "$RESOURCE_NAME" == "null" ]]; then
  cat <<EOF
{
  "error": "No matching resource found",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "expected_gateway": "$EXPECTED_GW",
  "expected_route": "$EXPECTED_RT",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, annotations: .metadata.annotations}]')
}
EOF
  exit 1
fi

printf '{"name":"%s"}\n' "$RESOURCE_NAME"
