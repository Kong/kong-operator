# abort on nonzero exitstatus, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Required environment variables.
NAMESPACE="${NAMESPACE:-}"
# Use the full resource name: e.g. kongservices.configuration.konghq.com
RESOURCE_TYPE="${RESOURCE_TYPE:-}" 
GATEWAY_NAME="${GATEWAY_NAME:-}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE:-}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME:-}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE:-}"

if [[ -z "$NAMESPACE" || -z "$RESOURCE_TYPE" || -z "$GATEWAY_NAME" || -z "$GATEWAY_NAMESPACE" || -z "$HTTP_ROUTE_NAME" || -z "$HTTP_ROUTE_NAMESPACE" ]]; then
  echo "Usage: NAMESPACE, RESOURCE_TYPE, GATEWAY_NAME, GATEWAY_NAMESPACE, HTTP_ROUTE_NAME, HTTP_ROUTE_NAMESPACE must be set" >&2
  exit 1
fi

EXPECTED_GW="${GATEWAY_NAMESPACE}/${GATEWAY_NAME}"
EXPECTED_RT="${HTTP_ROUTE_NAMESPACE}/${HTTP_ROUTE_NAME}"

# Fetch all resources and filter by the specific annotation CSV logic.
RESOURCE_NAME=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -o json | jq -r --arg gw "$EXPECTED_GW" --arg rt "$EXPECTED_RT" '
  .items[] | 
  select(
    (.metadata.annotations["gateway-operator.konghq.com/hybrid-gateways"] // "" | split(",") | contains([$gw])) and
    (.metadata.annotations["gateway-operator.konghq.com/hybrid-routes"] // "" | split(",") | contains([$rt]))
  ) | .metadata.name' | head -n 1)

if [[ -z "$RESOURCE_NAME" || "$RESOURCE_NAME" == "null" || "$RESOURCE_NAME" == "" ]]; then
  echo "Error: No $RESOURCE_TYPE found for $EXPECTED_GW and $EXPECTED_RT" >&2
  exit 1
fi

printf '{"name":"%s"}\n' "$RESOURCE_NAME"