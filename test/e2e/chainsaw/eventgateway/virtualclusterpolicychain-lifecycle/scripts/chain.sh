#!/bin/bash
# Observe the declared cluster-level policy order and, after chain CR deletion,
# the retained policies. Prints their Konnect IDs, UIDs, and API paths as JSON.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: Namespace of the chain and policies.
#   VIRTUAL_CLUSTER_NAME: Parent virtual cluster name.
#   KONNECT_TOKEN, KONNECT_URL: Konnect API token and host.
#   ORIGINAL: Required in "retained" mode; JSON output of "verify".
#   WINDOW_SECONDS: (optional) Observation window. Default: 75 seconds,
#     longer than the default 60-second Konnect sync period.
#   CHECK_INTERVAL: (optional) Seconds between checks. Default: 5.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
MODE="${1:?expected verify or retained}"
WINDOW_SECONDS="${WINDOW_SECONDS:-75}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5}"

fail() {
  jq -n --arg error "$1" '{error: $error}'
  exit 1
}

policy() {
  local response
  if ! response="$(RESOURCE_TYPE=eventgatewayvirtualclusterpolicies.configuration.konghq.com \
    RESOURCE_NAME="$1" COLLECTION_PATH_JQ='"/v1/event-gateways/" + .status.gatewayID.id + "/virtual-clusters/" + .status.virtualClusterID.id + "/cluster-policies"' \
    bash "${COMMON}/konnect_entity_of_resource.sh")"; then
    echo "${response}" >&2
    return 1
  fi
  echo "${response}"
}

FIRST="$(policy first)"
SECOND="$(policy second)"
export FIRST_ID SECOND_ID
FIRST_ID="$(jq -er '.id' <<<"${FIRST}")"
SECOND_ID="$(jq -er '.id' <<<"${SECOND}")"
[[ "${FIRST_ID}" != "${SECOND_ID}" ]] || fail "policies must have distinct Konnect IDs"
COLLECTION="$(jq -er '.collection_path' <<<"${FIRST}")"
[[ "${COLLECTION}" == "$(jq -er '.collection_path' <<<"${SECOND}")" ]] || fail "policies must share the same parent"
export API_PATH="${COLLECTION%/cluster-policies}/cluster-policy-chain"
# shellcheck disable=SC2016 # jq, not the shell, reads $ENV.
EXPECTED_JQ='.policies == [$ENV.SECOND_ID, $ENV.FIRST_ID]'
export EXPECTED_JQ

assert_programmed() {
  RESOURCE_TYPE=eventgatewayvirtualclusterpolicychains.configuration.konghq.com \
    RESOURCE_NAME=chain MAX_RETRIES=1 RETRY_DELAY=0 JQ_CONDITION='
    .metadata.generation as $generation
    | ((.status.id // "") == "")
      and any(.status.conditions[]; .type == "Programmed" and .status == "True" and .observedGeneration == $generation)
      and any(.status.conditions[]; .type == "KonnectReferencesResolved" and .status == "True" and .observedGeneration == $generation)
      and any(.status.conditions[]; .type == "PolicyChainOwnerValid" and .status == "True" and .observedGeneration == $generation)
    ' bash "${COMMON}/wait_for_resource_reconciled.sh" >&2
}

SNAPSHOT="$(jq -cn --argjson first "${FIRST}" --argjson second "${SECOND}" --arg path "${API_PATH}" \
  '{first: $first, second: $second, chain_path: $path}')"
case "${MODE}" in
  verify)
    assert_programmed
    ;;
  retained)
    jq -en --argjson original "${ORIGINAL:?ORIGINAL is required}" --argjson current "${SNAPSHOT}" \
      '$original == $current' >/dev/null || fail "chain deletion changed a policy UID, Konnect ID, or parent"
    ;;
  *)
    fail "unknown mode ${MODE}"
    ;;
esac

# First wait for convergence (including the periodic drift repair), then require
# every GET in a full sync-period window to match; one lucky GET is not enough.
RETRY_COUNT=150 bash "${COMMON}/konnect_api_request.sh" >&2
START="${SECONDS}"
while true; do
  RETRY_COUNT=1 bash "${COMMON}/konnect_api_request.sh" >&2
  if [[ "${MODE}" == "retained" ]]; then
    CHAIN="$(kubectl get eventgatewayvirtualclusterpolicychains.configuration.konghq.com chain -n "${NAMESPACE}" --ignore-not-found -o name)"
    [[ -z "${CHAIN}" ]] || fail "deleted chain CR reappeared"
    for NAME in first second; do
      RESOURCE="$(kubectl get eventgatewayvirtualclusterpolicies.configuration.konghq.com "${NAME}" -n "${NAMESPACE}" -o json)"
      jq -e --argjson original "${ORIGINAL}" --arg name "${NAME}" '
        .metadata.uid == $original[$name].uid
          and .status.id == $original[$name].id
          and .metadata.deletionTimestamp == null
      ' <<<"${RESOURCE}" >/dev/null || fail "retained policy ${NAME} was replaced or deleted"
      POLICY_ID="$(jq -er '.status.id' <<<"${RESOURCE}")"
      API_PATH="${COLLECTION}/${POLICY_ID}" WANT_POLICY_ID="${POLICY_ID}" \
        EXPECTED_JQ='.id == $ENV.WANT_POLICY_ID' RETRY_COUNT=1 \
        bash "${COMMON}/konnect_api_request.sh" >&2
    done
    kubectl get eventgatewayvirtualclusters.configuration.konghq.com "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
      jq -e '.metadata.deletionTimestamp == null and .metadata.annotations["gateway-operator.konghq.com/cluster-policy-chain-owner"] == null' \
        >/dev/null || fail "chain deletion did not release ownership of the retained virtual cluster"
  else
    assert_programmed
  fi
  [[ $((SECONDS - START)) -ge "${WINDOW_SECONDS}" ]] && break
  sleep "${CHECK_INTERVAL}"
done
jq -cn --argjson snapshot "${SNAPSHOT}" '$snapshot'
