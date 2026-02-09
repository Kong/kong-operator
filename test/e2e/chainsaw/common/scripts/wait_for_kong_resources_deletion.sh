#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCES_JSON: JSON string from get_kong_resources.sh containing resources to wait for deletion.
#   TIMEOUT: (Optional) Timeout in seconds. Default: 180.

RESOURCES_JSON="${RESOURCES_JSON}"
TIMEOUT="${TIMEOUT:-180}"

# Parse the JSON to extract resource types and names.
RESOURCE_COUNT=$(echo "$RESOURCES_JSON" | jq -r '.resources | length')

if [[ "$RESOURCE_COUNT" -eq 0 ]]; then
  jq -n '{status: "success", message: "no resources to delete"}'
  exit 0
fi

# Wait for all resources to be deleted.
DELETED=()
FAILED=()

for i in $(seq 0 $((RESOURCE_COUNT - 1))); do
  RESOURCE_TYPE=$(echo "$RESOURCES_JSON" | jq -r ".resources[$i].type")
  RESOURCE_NAME=$(echo "$RESOURCES_JSON" | jq -r ".resources[$i].name")
  RESOURCE_NAMESPACE=$(echo "$RESOURCES_JSON" | jq -r ".resources[$i].namespace")
  
  # Skip if name is null or empty
  if [[ "$RESOURCE_NAME" == "null" ]] || [[ -z "$RESOURCE_NAME" ]]; then
    continue
  fi
  
  RESOURCE_ID="$RESOURCE_TYPE/$RESOURCE_NAME -n $RESOURCE_NAMESPACE"
  
  if ERROR=$(kubectl wait --for=delete $RESOURCE_ID --timeout="${TIMEOUT}s" 2>&1); then
    DELETED+=("$RESOURCE_ID")
  else
    FAILED+=("$RESOURCE_ID: $ERROR")
  fi
done

# Output result as JSON
if [[ ${#FAILED[@]} -eq 0 ]]; then
  jq -n \
    --argjson deleted "$(printf '%s\n' "${DELETED[@]}" | jq -R . | jq -s .)" \
    '{status: "success", message: "all resources deleted", deleted: $deleted}'
  exit 0
else
  jq -n \
    --argjson deleted "$(printf '%s\n' "${DELETED[@]}" | jq -R . | jq -s .)" \
    --argjson failed "$(printf '%s\n' "${FAILED[@]}" | jq -R . | jq -s .)" \
    '{status: "error", message: "failed to delete some resources", deleted: $deleted, failed: $failed}'
  exit 1
fi
