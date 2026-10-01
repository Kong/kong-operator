#!/bin/bash
# Resolve the Konnect entity behind a Kubernetes resource managed by the
# operator: its Konnect ID (status.id), its Kubernetes UID and the Konnect API
# path of the entity collection it belongs to. The collection path is built
# from the resource itself with a jq expression, typically from the parent IDs
# the operator records in its status, e.g.
#   '"/v1/ai-gateways/" + .status.gatewayID.id + "/consumers"'
#
# Prints {"id": <Konnect ID or null>, "uid": <UID>,
# "collection_path": <path>, "entity_path": <collection_path/id or null>}.
# Retries until the collection path can be built and, unless REQUIRE_ID is
# "false", until the Konnect ID is set.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewayconsumers.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The resource namespace.
#   COLLECTION_PATH_JQ: jq expression, evaluated against the resource JSON,
#     that yields the Konnect API path of the entity collection. It must yield
#     null (or fail) while the parent IDs it needs are not set yet.
#   REQUIRE_ID: (optional) "false" to not wait for status.id. Default: true.
#   MAX_RETRIES: (optional) Number of attempts. Default: 60.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
COLLECTION_PATH_JQ="${COLLECTION_PATH_JQ}"
REQUIRE_ID="${REQUIRE_ID:-true}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-1}"

KUBECTL_OUTPUT=""
for _ in $(seq 1 "${MAX_RETRIES}"); do
  if KUBECTL_OUTPUT="$(kubectl get "${RESOURCE_TYPE}" "${RESOURCE_NAME}" -n "${NAMESPACE}" -o json 2>&1)" &&
    RESULT="$(jq -ce "
      (try (${COLLECTION_PATH_JQ}) catch null) as \$collection
      | (.status.id // null) as \$id
      | select(\$collection != null and (\$id != null or \"${REQUIRE_ID}\" == \"false\"))
      | {id: \$id, uid: .metadata.uid, collection_path: \$collection,
         entity_path: (if \$id then \$collection + \"/\" + \$id else null end)}
    " <<<"${KUBECTL_OUTPUT}")"; then
    echo "${RESULT}"
    exit 0
  fi
  sleep "${RETRY_DELAY}"
done

jq -n \
  --arg error "cannot resolve the Konnect entity of ${RESOURCE_TYPE} ${NAMESPACE}/${RESOURCE_NAME}" \
  --arg collection_path_jq "${COLLECTION_PATH_JQ}" \
  --arg kubectl_output "${KUBECTL_OUTPUT}" \
  '{error: $error, collection_path_jq: $collection_path_jq, kubectl_output: $kubectl_output}'
exit 1
