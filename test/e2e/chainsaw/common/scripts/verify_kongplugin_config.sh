#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies the configuration of the KongPlugin the operator mirrors into the control plane namespace
# for a given route and plugin type. It is selected by the managed-by label plus the hybrid-routes
# annotation, so user-authored KongPlugins in the same namespace are never considered.
#
# Variables (from environment):
#   NAMESPACE: Namespace holding the mirrored KongPlugin (the control plane namespace).
#   ROUTE_REF: "<namespace>/<name>" of the route the plugin is attached to.
#   PLUGIN_TYPE: Kong plugin type, e.g. 'response-transformer'.
#   EXPECTED_CONFIG: JSON object the mirrored KongPlugin config must be equal to.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
ROUTE_REF="${ROUTE_REF}"
PLUGIN_TYPE="${PLUGIN_TYPE}"
EXPECTED_CONFIG="${EXPECTED_CONFIG}"
RETRY_COUNT="${RETRY_COUNT:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

MANAGED_BY_LABEL="gateway-operator.konghq.com/managed-by"
ROUTES_ANNOTATION="gateway-operator.konghq.com/hybrid-routes"

# jq --argjson aborts on malformed input, so report it as a normal failure instead.
if ! jq -e . >/dev/null 2>&1 <<<"$EXPECTED_CONFIG"; then
  jq -n --arg expected_config "$EXPECTED_CONFIG" \
    '{success: false, error: "EXPECTED_CONFIG is not valid JSON", expected_config: $expected_config}'
  exit 0
fi

ATTEMPT=0
LAST_ERROR=""
LAST_CONFIG="null"

while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

  PLUGINS_JSON=$(kubectl get kongplugins.configuration.konghq.com -n "$NAMESPACE" \
    -l "${MANAGED_BY_LABEL}" -o json 2>&1) || {
    LAST_ERROR="failed to list KongPlugins: ${PLUGINS_JSON}"
    sleep "$RETRY_DELAY"
    continue
  }

  # The annotation is a bare comma-separated list, so match a whole element: a substring match
  # would also accept a route whose name merely starts with this one.
  LAST_CONFIG=$(echo "$PLUGINS_JSON" | jq -c \
    --arg route "$ROUTE_REF" \
    --arg ptype "$PLUGIN_TYPE" \
    --arg ann "$ROUTES_ANNOTATION" '
      [
        .items[]
        | select(.plugin == $ptype)
        | select((.metadata.annotations[$ann] // "") | split(",") | index($route))
      ][0].config // null')

  if [[ "$LAST_CONFIG" == "null" ]]; then
    LAST_ERROR="no mirrored KongPlugin of type \"${PLUGIN_TYPE}\" for route \"${ROUTE_REF}\" with a config was found yet"
    sleep "$RETRY_DELAY"
    continue
  fi

  if echo "$LAST_CONFIG" | jq -e --argjson expected "$EXPECTED_CONFIG" '. == $expected' >/dev/null; then
    jq -n \
      --arg namespace "$NAMESPACE" \
      --arg route_ref "$ROUTE_REF" \
      --arg plugin_type "$PLUGIN_TYPE" \
      --argjson config "$LAST_CONFIG" \
      --argjson retry_attempt "$ATTEMPT" \
      '{success: true, namespace: $namespace, route_ref: $route_ref, plugin_type: $plugin_type, config: $config, retry_attempt: $retry_attempt}'
    exit 0
  fi

  LAST_ERROR="mirrored KongPlugin config does not match the expected one yet"
  sleep "$RETRY_DELAY"
done

# LAST_ERROR embeds quotes and raw kubectl stderr, so let jq escape it.
jq -n \
  --arg error "$LAST_ERROR" \
  --arg namespace "$NAMESPACE" \
  --arg route_ref "$ROUTE_REF" \
  --arg plugin_type "$PLUGIN_TYPE" \
  --argjson expected_config "$EXPECTED_CONFIG" \
  --argjson last_config "$LAST_CONFIG" \
  --argjson retry_count "$RETRY_COUNT" \
  '{success: false, error: $error, namespace: $namespace, route_ref: $route_ref, plugin_type: $plugin_type, expected_config: $expected_config, last_config: $last_config, retry_count: $retry_count}'
exit 0
