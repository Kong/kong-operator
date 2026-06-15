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
#   HTTP_ROUTE_NAME: (Optional) The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: (Optional) The namespace of the HTTPRoute.
#   EXPECTED_SERVICE_NAME: (Optional) The KongService name referenced by a KongRoute.
#   REQUIRED_CONDITION_TYPE: (Optional) Condition type that the resource must have.
#   REQUIRED_CONDITION_STATUS: (Optional) Required condition status. Default: True.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
RESOURCE_TYPE="${RESOURCE_TYPE}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME:-}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE:-}"
EXPECTED_SERVICE_NAME="${EXPECTED_SERVICE_NAME:-}"
REQUIRED_CONDITION_TYPE="${REQUIRED_CONDITION_TYPE:-}"
REQUIRED_CONDITION_STATUS="${REQUIRED_CONDITION_STATUS:-True}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT=""
if [[ -n "$HTTP_ROUTE_NAME" && -n "$HTTP_ROUTE_NAMESPACE" ]]; then
  EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"
fi

ATTEMPT=0
RESOURCE_NAME=""

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))
  
  # Fetch all resources.
  KUBECTL_CMD="kubectl get $RESOURCE_TYPE -n $NAMESPACE -o json"
  if ! KUBECTL_OUTPUT=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -o json 2>&1); then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "Failed to get resource after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD",
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
  "namespace": "$NAMESPACE",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  # Find resource that matches annotations
  # If route is specified, require both gateway and route annotations
  # If route is not specified, only require gateway annotation
  RESOURCE_INFO=$(echo "$KUBECTL_OUTPUT" | jq -r \
    --arg gw "$EXPECTED_GW" \
    --arg rt "$EXPECTED_RT" \
    --arg service "$EXPECTED_SERVICE_NAME" \
    --arg condition_type "$REQUIRED_CONDITION_TYPE" \
    --arg condition_status "$REQUIRED_CONDITION_STATUS" '
      [
        .items[]
        | select(
          (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw]))
          and ($rt == "" or (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt])))
          and ($service == "" or .spec.serviceRef.namespacedRef.name == $service)
          and (
            $condition_type == ""
            or any(.status.conditions[]?; .type == $condition_type and .status == $condition_status)
          )
        )
        | {name: .metadata.name, kind: .kind, namespace: .metadata.namespace}
      ][0] // empty
      | if type == "object" then @json else empty end')

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
  "namespace": "$RESOURCE_NAMESPACE",
  "retry_attempt": $ATTEMPT,
  "max_retries": $RETRY_COUNT,
  "kubectl_command": "kubectl get $RESOURCE_TYPE -n $NAMESPACE -o json"
}
EOF
    exit 0
  fi

  # If this is the last attempt, provide detailed diagnostics
  if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
    if [[ -n "$EXPECTED_RT" ]]; then
      cat <<EOF
{
  "error": "No matching resource found after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "expected_gateway": "$EXPECTED_GW",
  "expected_route": "$EXPECTED_RT",
  "expected_service_name": "$EXPECTED_SERVICE_NAME",
  "required_condition_type": "$REQUIRED_CONDITION_TYPE",
  "required_condition_status": "$REQUIRED_CONDITION_STATUS",
  "kubectl_command": "$KUBECTL_CMD",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, annotations: .metadata.annotations, serviceRef: .spec.serviceRef, conditions: .status.conditions}]')
}
EOF
    else
      cat <<EOF
{
  "error": "No matching resource found after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "expected_gateway": "$EXPECTED_GW",
  "expected_service_name": "$EXPECTED_SERVICE_NAME",
  "required_condition_type": "$REQUIRED_CONDITION_TYPE",
  "required_condition_status": "$REQUIRED_CONDITION_STATUS",
  "kubectl_command": "$KUBECTL_CMD",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, annotations: .metadata.annotations, serviceRef: .spec.serviceRef, conditions: .status.conditions}]')
}
EOF
    fi
    exit 1
  fi

  sleep "$RETRY_DELAY"
done
