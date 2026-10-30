#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies that a field configured on a KongPlugin propagated all the way to the
# real Konnect-side plugin entity, by resolving the KongPluginBinding generated for
# it and querying the Konnect Admin API directly.
#
# Variables (from environment):
#   NAMESPACE: The namespace to search for the KongPluginBinding.
#   PLUGIN_REFERENCE_NAME: The name of the KongPlugin referenced by the binding
#     (substring match on spec.pluginRef.name).
#   KONNECT_TOKEN: Bearer token for the Konnect Admin API.
#   KONNECT_URL: Konnect server host, e.g. eu.api.konghq.tech.
#   FIELD: Path of the Konnect plugin field to check, e.g. .tags or .ordering.
#   EXPECTED: JSON value the field is compared with, e.g. ["a","b"] or null.
#   MATCH: (Optional) How the field is compared with EXPECTED. Default: equals.
#     equals: the values are equal once nulls, empty arrays and empty objects
#       are pruned on both sides, so {} or [] from Konnect equal null.
#     includes: EXPECTED is an array whose elements must all be in the field.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
PLUGIN_REFERENCE_NAME="${PLUGIN_REFERENCE_NAME}"
KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
FIELD="${FIELD}"
EXPECTED="${EXPECTED}"
MATCH="${MATCH:-equals}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# FIELD is spliced into a jq program, so only plain object paths are accepted.
if ! [[ "$FIELD" =~ ^(\.[a-z_]+)+$ ]]; then
  echo "FIELD must be a plain jq object path such as .tags, got: ${FIELD}" >&2
  exit 1
fi

PRUNE='def prune:
  if type == "object" then
    map_values(prune) | with_entries(select(.value != null)) | if length == 0 then null else . end
  elif type == "array" then
    map(prune) | map(select(. != null)) | if length == 0 then null else . end
  else . end;'

case "$MATCH" in
  equals)
    MATCHES="${PRUNE} (${FIELD} | prune) == (\$expected | prune)"
    ;;
  includes)
    MATCHES="(\$expected - (${FIELD} // [])) | length == 0"
    ;;
  *)
    echo "MATCH must be equals or includes, got: ${MATCH}" >&2
    exit 1
    ;;
esac

EXPECTED_JSON=$(echo "$EXPECTED" | jq -c '.')
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

  if [[ "$HTTP_STATUS" != "200" ]]; then
    LAST_ERROR="Konnect API returned HTTP ${HTTP_STATUS}: $(cat "$RESPONSE_FILE")"
    sleep "$RETRY_DELAY"
    continue
  fi

  ACTUAL_JSON=$(jq -c "${FIELD}" "$RESPONSE_FILE")

  if jq -e --argjson expected "$EXPECTED_JSON" "$MATCHES" "$RESPONSE_FILE" > /dev/null; then
    jq -n \
      --arg plugin_id "$PLUGIN_ID" \
      --arg control_plane_id "$CONTROL_PLANE_ID" \
      --arg field "$FIELD" \
      --argjson value "$ACTUAL_JSON" \
      --argjson retry_attempt "$ATTEMPT" \
      '{success: true, plugin_id: $plugin_id, control_plane_id: $control_plane_id, field: $field, value: $value, retry_attempt: $retry_attempt}'
    exit 0
  fi

  LAST_ERROR="Konnect plugin ${PLUGIN_ID} field ${FIELD} is ${ACTUAL_JSON}, expected (${MATCH}) ${EXPECTED_JSON}"
  sleep "$RETRY_DELAY"
done

jq -n \
  --arg error "$LAST_ERROR" \
  --arg namespace "$NAMESPACE" \
  --arg plugin_reference_name "$PLUGIN_REFERENCE_NAME" \
  --arg field "$FIELD" \
  --arg match "$MATCH" \
  --argjson expected "$EXPECTED_JSON" \
  --argjson retry_count "$RETRY_COUNT" \
  '{error: $error, namespace: $namespace, plugin_reference_name: $plugin_reference_name, field: $field, match: $match, expected: $expected, retry_count: $retry_count}'
exit 1
