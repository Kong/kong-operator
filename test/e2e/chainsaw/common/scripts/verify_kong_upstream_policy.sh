#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for the KongUpstream.
#   GATEWAY_NAME: The name of the gateway.
#   GATEWAY_NAMESPACE: The namespace of the gateway.
#   HTTP_ROUTE_NAME: The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: The namespace of the HTTPRoute.
#   EXPECTED_ALGORITHM: (Optional) Expected algorithm value.
#   EXPECTED_SLOTS: (Optional) Expected slots value.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"
EXPECTED_ALGORITHM="${EXPECTED_ALGORITHM:-}"
EXPECTED_SLOTS="${EXPECTED_SLOTS:-}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"

ATTEMPT=0

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

  # Fetch all KongUpstreams in the namespace.
  if ! KUBECTL_OUTPUT=$(kubectl get kongupstreams.configuration.konghq.com -n "$NAMESPACE" -o json 2>&1); then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "Failed to get KongUpstreams after $RETRY_COUNT attempts",
  "kubectl_command": "kubectl get kongupstreams.configuration.konghq.com -n $NAMESPACE -o json",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  # Find KongUpstream that matches both gateway and route annotations.
  RESOURCE_INFO=$(echo "$KUBECTL_OUTPUT" | jq -r --arg gw "$EXPECTED_GW" --arg rt "$EXPECTED_RT" '
    .items[] |
    select(
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])) and
      (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt]))
    ) | {name: .metadata.name, namespace: .metadata.namespace, spec: .spec} | @json' | head -n 1)

  if [[ -z "$RESOURCE_INFO" || "$RESOURCE_INFO" == "null" ]]; then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "No matching KongUpstream found after $RETRY_COUNT attempts",
  "expected_gateway": "$EXPECTED_GW",
  "expected_route": "$EXPECTED_RT",
  "kubectl_command": "kubectl get kongupstreams.configuration.konghq.com -n $NAMESPACE -o json",
  "available_resources": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, annotations: .metadata.annotations}]')
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  # KongUpstream found. Now verify the spec fields.
  SPEC=$(echo "$RESOURCE_INFO" | jq -r '.spec')
  RESOURCE_NAME=$(echo "$RESOURCE_INFO" | jq -r '.name')

  # Verify algorithm if expected.
  if [[ -n "$EXPECTED_ALGORITHM" ]]; then
    ACTUAL_ALGORITHM=$(echo "$SPEC" | jq -r '.algorithm // ""')
    if [[ "$ACTUAL_ALGORITHM" != "$EXPECTED_ALGORITHM" ]]; then
      if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
        cat <<EOF
{
  "error": "Algorithm mismatch after $RETRY_COUNT attempts",
  "resource_name": "$RESOURCE_NAME",
  "expected_algorithm": "$EXPECTED_ALGORITHM",
  "actual_algorithm": "$ACTUAL_ALGORITHM",
  "kubectl_command": "kubectl get kongupstream $RESOURCE_NAME -n $NAMESPACE -o jsonpath={.spec}"
}
EOF
        exit 1
      fi
      sleep "$RETRY_DELAY"
      continue
    fi
  fi

  # Verify slots if expected.
  if [[ -n "$EXPECTED_SLOTS" ]]; then
    ACTUAL_SLOTS=$(echo "$SPEC" | jq -r '.slots // ""')
    if [[ "$ACTUAL_SLOTS" != "$EXPECTED_SLOTS" ]]; then
      if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
        cat <<EOF
{
  "error": "Slots mismatch after $RETRY_COUNT attempts",
  "resource_name": "$RESOURCE_NAME",
  "expected_slots": "$EXPECTED_SLOTS",
  "actual_slots": "$ACTUAL_SLOTS",
  "kubectl_command": "kubectl get kongupstream $RESOURCE_NAME -n $NAMESPACE -o jsonpath={.spec}"
}
EOF
        exit 1
      fi
      sleep "$RETRY_DELAY"
      continue
    fi
  fi

  # All checks passed.
  cat <<EOF
{
  "success": true,
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE",
  "algorithm": "$ACTUAL_ALGORITHM",
  "slots": "$ACTUAL_SLOTS",
  "retry_attempt": $ATTEMPT,
  "max_retries": $RETRY_COUNT
}
EOF
  exit 0
done
