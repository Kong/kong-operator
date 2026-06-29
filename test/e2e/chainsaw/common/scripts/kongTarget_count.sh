#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for KongTargets.
#   HTTP_ROUTE_NAME: The name of the HTTPRoute (used when ROUTE_TYPE=HTTPRoute).
#   HTTP_ROUTE_NAMESPACE: The namespace of the HTTPRoute (used when ROUTE_TYPE=HTTPRoute).
#   TLS_ROUTE_NAME: The name of the TLSRoute (used when ROUTE_TYPE=TLSRoute).
#   TLS_ROUTE_NAMESPACE: The namespace of the TLSRoute (used when ROUTE_TYPE=TLSRoute).
#   ROUTE_TYPE: (Optional) "HTTPRoute" (default) or "TLSRoute".
#   EXPECTED_COUNT: The expected number of KongTargets.
#   TARGET_WEIGHT: Filter by specific weight. Use "" (empty string) to count all targets.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
EXPECTED_COUNT="${EXPECTED_COUNT}"
TARGET_WEIGHT="${TARGET_WEIGHT:-}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
ROUTE_TYPE="${ROUTE_TYPE:-HTTPRoute}"

if [[ "$ROUTE_TYPE" == "TLSRoute" ]]; then
  ROUTE_NAME="${TLS_ROUTE_NAME}"
  ROUTE_NAMESPACE="${TLS_ROUTE_NAMESPACE}"
  ANNOTATION_KEY="gateway-operator.konghq.com/hybrid-routes-TLSRoute"
  ROUTE_FIELD="tls_route"
else
  ROUTE_NAME="${HTTP_ROUTE_NAME}"
  ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"
  ANNOTATION_KEY="gateway-operator.konghq.com/hybrid-routes"
  ROUTE_FIELD="http_route"
fi

EXPECTED_ROUTE="${ROUTE_NAMESPACE}/${ROUTE_NAME}"

ATTEMPT=0

KUBECTL_CMD="kubectl get kongtarget -n $NAMESPACE -o json"

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

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

  if [[ -n "$TARGET_WEIGHT" ]]; then
    ACTUAL_COUNT=$(echo "$KUBECTL_OUTPUT" | jq -r --arg route "$EXPECTED_ROUTE" --argjson weight "$TARGET_WEIGHT" \
      --arg key "$ANNOTATION_KEY" \
      '[.items[] | select(
        (.metadata.annotations[$key] // "" | split(",") | contains([$route])) and
        (.spec.weight == $weight)
      )] | length')
  else
    ACTUAL_COUNT=$(echo "$KUBECTL_OUTPUT" | jq -r --arg route "$EXPECTED_ROUTE" \
      --arg key "$ANNOTATION_KEY" \
      '[.items[] | select(.metadata.annotations[$key] // "" | split(",") | contains([$route]))] | length')
  fi

  if [[ "$ACTUAL_COUNT" == "$EXPECTED_COUNT" ]]; then
    cat <<EOF
{
  "success": true,
  "count": $ACTUAL_COUNT,
  "expected": $EXPECTED_COUNT,
  "$ROUTE_FIELD": "$EXPECTED_ROUTE",
  "weight_filter": $(if [[ -n "$TARGET_WEIGHT" ]]; then echo "$TARGET_WEIGHT"; else echo "null"; fi),
  "retry_attempt": $ATTEMPT,
  "max_retries": $RETRY_COUNT,
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
    exit 0
  fi

  if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
    AVAILABLE_TARGETS=$(echo "$KUBECTL_OUTPUT" | jq -c --arg key "$ANNOTATION_KEY" \
      '[.items[] | {name: .metadata.name, weight: .spec.weight, routes: .metadata.annotations[$key]}]')
    WEIGHT_MSG=""
    if [[ -n "$TARGET_WEIGHT" ]]; then
      WEIGHT_MSG=" with weight $TARGET_WEIGHT"
    fi
    cat <<EOF
{
  "error": "Expected $EXPECTED_COUNT KongTargets for $ROUTE_TYPE $EXPECTED_ROUTE${WEIGHT_MSG}, but found $ACTUAL_COUNT after $RETRY_COUNT attempts",
  "namespace": "$NAMESPACE",
  "$ROUTE_FIELD": "$EXPECTED_ROUTE",
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
