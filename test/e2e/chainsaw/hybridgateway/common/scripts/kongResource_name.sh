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
#   RETRY_COUNT: (Optional) Number of retries. Default: 30
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 2

NAMESPACE="${NAMESPACE}"
RESOURCE_TYPE="${RESOURCE_TYPE}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"
RETRY_COUNT="${RETRY_COUNT:-30}"
RETRY_DELAY="${RETRY_DELAY:-2}"

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"

ATTEMPT=0
RESOURCE_NAME=""

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))
  
  # Fetch all resources.
  if ! KUBECTL_OUTPUT=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -o json 2>&1); then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "Failed to get resource after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  # First, check if any resources exist
  RESOURCE_COUNT=$(echo "$KUBECTL_OUTPUT" | jq '.items | length')
  if [[ "$RESOURCE_COUNT" -eq 0 ]]; then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "No resources of type $RESOURCE_TYPE found in namespace $NAMESPACE after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE"
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  # Find resource that matches annotations
  RESOURCE_INFO=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw "$EXPECTED_GW" --arg rt "$EXPECTED_RT" '
    .items[] | 
    select(
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])) and
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt]))
    ) | {name: .metadata.name, kind: .kind, namespace: .metadata.namespace} | @json' | head -n 1)

  # If resource found, we're done
  if [[ -n "$RESOURCE_INFO" && "$RESOURCE_INFO" != "null" ]]; then
    RESOURCE_NAME=$(echo "$RESOURCE_INFO" | jq -r '.name')
    RESOURCE_KIND=$(echo "$RESOURCE_INFO" | jq -r '.kind')
    RESOURCE_NAMESPACE=$(echo "$RESOURCE_INFO" | jq -r '.namespace')
    cat <<EOF
{
  "success": true,
  "kind": "$RESOURCE_KIND",
  "name": "$RESOURCE_NAME",
  "namespace": "$RESOURCE_NAMESPACE"
}
EOF
    exit 0
  fi

  # If this is the last attempt, provide detailed diagnostics
  if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
    cat <<EOF
{
  "error": "No matching resource found after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "expected_gateway": "$EXPECTED_GW",
  "expected_route": "$EXPECTED_RT",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, annotations: .metadata.annotations}]')
}
EOF
    exit 1
  fi

  sleep "$RETRY_DELAY"
done
