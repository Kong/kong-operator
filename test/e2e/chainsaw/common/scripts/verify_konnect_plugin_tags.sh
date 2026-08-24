#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies that the tags configured on a KongPlugin propagated all the way to the
# real Konnect-side plugin entity, by resolving the KongPluginBinding generated for
# it and querying the Konnect Admin API directly.
#
# Variables (from environment):
#   NAMESPACE: The namespace to search for the KongPluginBinding.
#   PLUGIN_REFERENCE_NAME: The name of the KongPlugin referenced by the binding
#     (spec.pluginReference.name).
#   KONNECT_TOKEN: Bearer token for the Konnect Admin API.
#   KONNECT_URL: Konnect server host, e.g. eu.api.konghq.tech.
#   EXPECTED_TAGS: Comma-separated list of tags that must all be present on the
#     Konnect-side plugin's tags array.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
PLUGIN_REFERENCE_NAME="${PLUGIN_REFERENCE_NAME}"
KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
EXPECTED_TAGS="${EXPECTED_TAGS}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

IFS=',' read -r -a EXPECTED_TAGS_ARRAY <<< "${EXPECTED_TAGS}"

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
        | select(.spec.pluginReference.name == $plugin_name)
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

  HTTP_STATUS=$(curl -s -o /tmp/verify_konnect_plugin_tags_response.json -w '%{http_code}' \
    -H "Authorization: Bearer ${KONNECT_TOKEN}" \
    "https://${KONNECT_URL}/v2/control-planes/${CONTROL_PLANE_ID}/core-entities/plugins/${PLUGIN_ID}") || true
  RESPONSE_BODY=$(cat /tmp/verify_konnect_plugin_tags_response.json)

  if [[ "$HTTP_STATUS" != "200" ]]; then
    LAST_ERROR="Konnect API returned HTTP ${HTTP_STATUS}: ${RESPONSE_BODY}"
    sleep "$RETRY_DELAY"
    continue
  fi

  MISSING_TAGS=()
  for expected_tag in "${EXPECTED_TAGS_ARRAY[@]}"; do
    if ! echo "$RESPONSE_BODY" | jq -e --arg tag "$expected_tag" '.tags // [] | index($tag) != null' > /dev/null; then
      MISSING_TAGS+=("$expected_tag")
    fi
  done

  if [[ ${#MISSING_TAGS[@]} -eq 0 ]]; then
    cat <<EOF
{
  "success": true,
  "plugin_id": "$PLUGIN_ID",
  "control_plane_id": "$CONTROL_PLANE_ID",
  "tags": $(echo "$RESPONSE_BODY" | jq -c '.tags // []'),
  "retry_attempt": $ATTEMPT
}
EOF
    exit 0
  fi

  LAST_ERROR="Konnect plugin ${PLUGIN_ID} is missing expected tags: $(printf '%s,' "${MISSING_TAGS[@]}")"
  sleep "$RETRY_DELAY"
done

cat <<EOF
{
  "error": "$LAST_ERROR",
  "namespace": "$NAMESPACE",
  "plugin_reference_name": "$PLUGIN_REFERENCE_NAME",
  "expected_tags": "$EXPECTED_TAGS",
  "retry_count": $RETRY_COUNT
}
EOF
exit 1
