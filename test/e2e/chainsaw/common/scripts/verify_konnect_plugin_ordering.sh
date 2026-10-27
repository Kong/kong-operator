#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies that the ordering configured on a KongPlugin propagated all the way to the
# real Konnect-side plugin entity, by resolving the KongPluginBinding generated for
# it and querying the Konnect Admin API directly.
#
# Variables (from environment):
#   NAMESPACE: The namespace to search for the KongPluginBinding.
#   PLUGIN_REFERENCE_NAME: The name of the KongPlugin referenced by the binding
#     (substring match on spec.pluginRef.name).
#   KONNECT_TOKEN: Bearer token for the Konnect Admin API.
#   KONNECT_URL: Konnect server host, e.g. eu.api.konghq.tech.
#   EXPECTED_ORDERING: JSON of the expected Konnect plugin ordering, e.g.
#     {"before":{"access":["key-auth"]}}, or null when no ordering is expected.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
PLUGIN_REFERENCE_NAME="${PLUGIN_REFERENCE_NAME}"
KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
EXPECTED_ORDERING="${EXPECTED_ORDERING}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Drops empty phase lists and empty before/after objects so that Konnect returning
# {} or empty lists compares equal to null.
NORMALIZE='def norm:
  if . == null then null
  else
    with_entries(.value |= ((. // {}) | with_entries(select((.value // []) | length > 0))))
    | with_entries(select(.value | length > 0))
    | if length == 0 then null else . end
  end;'

EXPECTED_NORMALIZED=$(echo "$EXPECTED_ORDERING" | jq -c "${NORMALIZE} norm")
RESPONSE_FILE=$(mktemp)
trap 'rm -f "$RESPONSE_FILE"' EXIT

ATTEMPT=0
LAST_ERROR=""

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

  BINDINGS_JSON=$(kubectl get kongpluginbindings.configuration.konghq.com -n "$NAMESPACE" -o json 2>&1) || {
    LAST_ERROR="failed to list KongPluginBindings: ${BINDINGS_JSON}"
    sleep "$RETRY_DELAY"
    continue
  }

  BINDING_INFO=$(echo "$BINDINGS_JSON" | jq -r \
    --arg plugin_name "$PLUGIN_REFERENCE_NAME" '
      [
        .items[]
        | select(.spec.pluginRef.name | contains($plugin_name))
        | select(.status.konnect.id != null and .status.konnect.controlPlaneID != null)
        | {id: .status.konnect.id, controlPlaneID: .status.konnect.controlPlaneID}
      ][0] // empty
      | if type == "object" then @json else empty end')

  if [[ -z "$BINDING_INFO" || "$BINDING_INFO" == "null" ]]; then
    LAST_ERROR="no KongPluginBinding referencing KongPlugin \"${PLUGIN_REFERENCE_NAME}\" with a populated Konnect status was found yet"
    sleep "$RETRY_DELAY"
    continue
  fi

  PLUGIN_ID=$(echo "$BINDING_INFO" | jq -r '.id')
  CONTROL_PLANE_ID=$(echo "$BINDING_INFO" | jq -r '.controlPlaneID')

  HTTP_STATUS=$(curl -s -o "$RESPONSE_FILE" -w '%{http_code}' \
    -H "Authorization: Bearer ${KONNECT_TOKEN}" \
    "https://${KONNECT_URL}/v2/control-planes/${CONTROL_PLANE_ID}/core-entities/plugins/${PLUGIN_ID}") || true
  RESPONSE_BODY=$(cat "$RESPONSE_FILE")

  if [[ "$HTTP_STATUS" != "200" ]]; then
    LAST_ERROR="Konnect API returned HTTP ${HTTP_STATUS}: ${RESPONSE_BODY}"
    sleep "$RETRY_DELAY"
    continue
  fi

  ACTUAL_NORMALIZED=$(echo "$RESPONSE_BODY" | jq -c "${NORMALIZE} .ordering | norm")

  if [[ "$ACTUAL_NORMALIZED" == "$EXPECTED_NORMALIZED" ]]; then
    cat <<EOF
{
  "success": true,
  "plugin_id": "$PLUGIN_ID",
  "control_plane_id": "$CONTROL_PLANE_ID",
  "ordering": $ACTUAL_NORMALIZED,
  "retry_attempt": $ATTEMPT
}
EOF
    exit 0
  fi

  LAST_ERROR="Konnect plugin ${PLUGIN_ID} ordering is ${ACTUAL_NORMALIZED}, expected ${EXPECTED_NORMALIZED}"
  sleep "$RETRY_DELAY"
done

jq -n \
  --arg error "$LAST_ERROR" \
  --arg namespace "$NAMESPACE" \
  --arg plugin_reference_name "$PLUGIN_REFERENCE_NAME" \
  --argjson expected_ordering "$EXPECTED_NORMALIZED" \
  --argjson retry_count "$RETRY_COUNT" \
  '{error: $error, namespace: $namespace, plugin_reference_name: $plugin_reference_name, expected_ordering: $expected_ordering, retry_count: $retry_count}'
exit 1
