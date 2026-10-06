#!/bin/bash
# Delete, from a Konnect entity collection, the entities whose MATCH_FIELD is
# MATCH_VALUE and that are not owned by the operator (they carry no k8s-uid
# label), e.g. an entity a test created directly in Konnect. Entities owned by
# the operator are never deleted.
#
# Prints {"deleted": [<IDs>]}.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   KONNECT_TOKEN: Bearer token for the Konnect API.
#   KONNECT_URL: Konnect server host, e.g. us.api.konghq.tech.
#   API_PATH: Path of the entity collection, e.g. '/v1/ai-gateways/<id>/consumers'.
#   MATCH_FIELD: (optional) Top-level field identifying the entities, e.g.
#     'title'. Default: name.
#   MATCH_VALUE: Value MATCH_FIELD must have.

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
API_PATH="${API_PATH}"
MATCH_FIELD="${MATCH_FIELD:-name}"
MATCH_VALUE="${MATCH_VALUE}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

LIST="$(RETRY_COUNT=1 EXPECTED_JQ='' bash "${SCRIPT_DIR}/konnect_list_all_entities.sh")"
IDS="$(jq -r --arg field "${MATCH_FIELD}" --arg value "${MATCH_VALUE}" '
  [(.body | if type == "array" then . else .data end)[]
    | select(.labels["k8s-uid"] == null and .[$field] == $value)
    | .id] | .[]' <<<"${LIST}")"

DELETED=()
for ID in ${IDS}; do
  METHOD=DELETE API_PATH="${API_PATH}/${ID}" RETRY_COUNT=1 BODY='' EXPECTED_JQ='' \
    EXPECTED_STATUS="200,204,404" bash "${SCRIPT_DIR}/konnect_api_request.sh" >/dev/null
  DELETED+=("${ID}")
done

# ${DELETED[@]+...} keeps an empty array from tripping nounset on bash < 4.4.
jq -n --args '{deleted: $ARGS.positional}' ${DELETED[@]+"${DELETED[@]}"}
