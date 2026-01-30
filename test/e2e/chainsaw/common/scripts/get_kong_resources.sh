#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search for the resources.
#   RESOURCE_TYPES: Comma-separated list of resource types, e.g. "kongservices.configuration.konghq.com,kongroutes.configuration.konghq.com".
#   GATEWAY_NAME: The name of the gateway.
#   GATEWAY_NAMESPACE: The namespace of the gateway.
#   HTTP_ROUTE_NAME: The name of the HTTPRoute.
#   HTTP_ROUTE_NAMESPACE: The namespace of the HTTPRoute.

NAMESPACE="${NAMESPACE}"
RESOURCE_TYPES="${RESOURCE_TYPES}"
GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE}"
HTTP_ROUTE_NAME="${HTTP_ROUTE_NAME}"
HTTP_ROUTE_NAMESPACE="${HTTP_ROUTE_NAMESPACE}"

# Get the directory where this script is located.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KONG_RESOURCE_NAME_SCRIPT="${SCRIPT_DIR}/kongResource_name.sh"

# Check if kongResource_name.sh exists.
if [[ ! -f "$KONG_RESOURCE_NAME_SCRIPT" ]]; then
  cat <<EOF
{
  "error": "kongResource_name.sh not found",
  "expected_path": "$KONG_RESOURCE_NAME_SCRIPT"
}
EOF
  exit 1
fi

# Split RESOURCE_TYPES by comma and iterate.
IFS=',' read -ra TYPES <<< "$RESOURCE_TYPES"

# Build array of JSON objects.
RESULTS=()

for RESOURCE_TYPE in "${TYPES[@]}"; do
  # Trim whitespace.
  RESOURCE_TYPE=$(echo "$RESOURCE_TYPE" | xargs)
  
  # Skip empty entries.
  if [[ -z "$RESOURCE_TYPE" ]]; then
    continue
  fi
  
  # Call kongResource_name.sh for this resource type.
  if RESULT=$(NAMESPACE="$NAMESPACE" \
              RESOURCE_TYPE="$RESOURCE_TYPE" \
              GATEWAY_NAME="$GATEWAY_NAME" \
              GATEWAY_NAMESPACE="$GATEWAY_NAMESPACE" \
              HTTP_ROUTE_NAME="$HTTP_ROUTE_NAME" \
              HTTP_ROUTE_NAMESPACE="$HTTP_ROUTE_NAMESPACE" \
              bash "$KONG_RESOURCE_NAME_SCRIPT" 2>&1); then
    # Extract the name from the result JSON.
    RESOURCE_NAME=$(echo "$RESULT" | jq -r '.name // empty')
    
    if [[ -n "$RESOURCE_NAME" ]]; then
      RESULTS+=("$(jq -n --arg type "$RESOURCE_TYPE" --arg name "$RESOURCE_NAME" --arg ns "$NAMESPACE" \
        '{type: $type, name: $name, namespace: $ns}')")
    else
      RESULTS+=("$(jq -n --arg type "$RESOURCE_TYPE" --argjson result "$(echo "$RESULT" | jq -c .)" \
        '{type: $type, error: "No name found in result", result: $result}')")
    fi
  else
    RESULTS+=("$(jq -n --arg type "$RESOURCE_TYPE" --arg details "$RESULT" \
      '{type: $type, error: "Failed to get resource name", details: $details}')")
  fi
done

# Output the final JSON.
jq -n --argjson resources "$(printf '%s\n' "${RESULTS[@]}" | jq -s .)" \
  '{resources: $resources}'
