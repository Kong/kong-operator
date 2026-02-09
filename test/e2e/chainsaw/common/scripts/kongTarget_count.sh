#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for KongTargets.
#   HTTP_ROUTE_NAME: The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: The namespace of the HTTPRoute.
#   EXPECTED_COUNT: The expected number of KongTargets.
#   TARGET_WEIGHT: Filter by specific weight. Use "" (empty string) to count all targets.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"
EXPECTED_COUNT="${EXPECTED_COUNT}"
TARGET_WEIGHT="${TARGET_WEIGHT:-}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

EXPECTED_ROUTE="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"

ATTEMPT=0

KUBECTL_CMD="kubectl get kongtarget -n $NAMESPACE -o json"

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

  # Fetch all KongTargets in the namespace.
  if ! KUBECTL_OUTPUT=$(kubectl get kongtarget -n "$NAMESPACE" -o json 2>&1); then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "error": "Failed to get KongTargets after $RETRY_COUNT attempts",
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

  # Count KongTargets that match the criteria.
  # Filter by HTTPRoute annotation, and optionally by weight.
  if [[ -n "$TARGET_WEIGHT" ]]; then
    ACTUAL_COUNT=$(echo "$KUBECTL_OUTPUT" | jq -r --arg route "$EXPECTED_ROUTE" --argjson weight "$TARGET_WEIGHT" \
      '[.items[] | select(
        (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$route])) and
        (.spec.weight == $weight)
      )] | length')
  else
    ACTUAL_COUNT=$(echo "$KUBECTL_OUTPUT" | jq -r --arg route "$EXPECTED_ROUTE" \
      '[.items[] | select(.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$route]))] | length')
  fi

  if [[ "$ACTUAL_COUNT" == "$EXPECTED_COUNT" ]]; then
    cat <<EOF
{
  "success": true,
  "count": $ACTUAL_COUNT,
  "expected": $EXPECTED_COUNT,
  "http_route": "$EXPECTED_ROUTE",
  "weight_filter": $(if [[ -n "$TARGET_WEIGHT" ]]; then echo "$TARGET_WEIGHT"; else echo "null"; fi),
  "retry_attempt": $ATTEMPT,
  "max_retries": $RETRY_COUNT,
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
    exit 0
  fi

  # If this is the last attempt, provide detailed diagnostics.
  if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
    AVAILABLE_TARGETS=$(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, weight: .spec.weight, routes: .metadata.annotations["gateway-operator.konghq.com/hybrid-routes"]}]')
    WEIGHT_MSG=""
    if [[ -n "$TARGET_WEIGHT" ]]; then
      WEIGHT_MSG=" with weight $TARGET_WEIGHT"
    fi
    cat <<EOF
{
  "error": "Expected $EXPECTED_COUNT KongTargets for HTTPRoute $EXPECTED_ROUTE${WEIGHT_MSG}, but found $ACTUAL_COUNT after $RETRY_COUNT attempts",
  "namespace": "$NAMESPACE",
  "http_route": "$EXPECTED_ROUTE",
  "weight_filter": $(if [[ -n "$TARGET_WEIGHT" ]]; then echo "$TARGET_WEIGHT"; else echo "null"; fi),
  "expected_count": $EXPECTED_COUNT,
  "actual_count": $ACTUAL_COUNT,
  "kubectl_command": "$KUBECTL_CMD",
  "available_targets": $AVAILABLE_TARGETS
}
EOF
    exit 1
  fi

  sleep "$RETRY_DELAY"
done
