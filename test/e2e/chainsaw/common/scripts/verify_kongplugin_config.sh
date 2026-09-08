#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Verifies the configuration of the KongPlugin the operator mirrors into the control plane
# namespace for a given route and plugin type.
#
# The mirrored KongPlugin is selected by the operator's managed-by label plus the hybrid-routes
# annotation, so user-authored KongPlugins living in the same namespace are never considered. That
# matters for plugins using spec.configFrom, which carry no spec.config of their own.
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

  LAST_CONFIG=$(echo "$PLUGINS_JSON" | jq -c \
    --arg route "$ROUTE_REF" \
    --arg ptype "$PLUGIN_TYPE" \
    --arg ann "$ROUTES_ANNOTATION" '
      [
        .items[]
        | select(.plugin == $ptype)
        | select((.metadata.annotations[$ann] // "") | contains($route))
      ][0].config // null')

  if [[ "$LAST_CONFIG" == "null" ]]; then
    LAST_ERROR="no mirrored KongPlugin of type \"${PLUGIN_TYPE}\" for route \"${ROUTE_REF}\" with a config was found yet"
    sleep "$RETRY_DELAY"
    continue
  fi

  if echo "$LAST_CONFIG" | jq -e --argjson expected "$EXPECTED_CONFIG" '. == $expected' >/dev/null; then
    cat <<EOF
{
  "success": true,
  "namespace": "$NAMESPACE",
  "route_ref": "$ROUTE_REF",
  "plugin_type": "$PLUGIN_TYPE",
  "config": $LAST_CONFIG,
  "retry_attempt": $ATTEMPT
}
EOF
    exit 0
  fi

  LAST_ERROR="mirrored KongPlugin config does not match the expected one yet"
  sleep "$RETRY_DELAY"
done

cat <<EOF
{
  "success": false,
  "error": "$LAST_ERROR",
  "namespace": "$NAMESPACE",
  "route_ref": "$ROUTE_REF",
  "plugin_type": "$PLUGIN_TYPE",
  "expected_config": $EXPECTED_CONFIG,
  "last_config": $LAST_CONFIG,
  "retry_count": $RETRY_COUNT
}
EOF
exit 0
