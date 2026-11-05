#!/bin/bash
# Clean up the dedicated gateway by its exact name and test label, including
# failures before status.id is persisted. Auth is cleaned up by earlier steps.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: Namespace of the test resources.
#   GATEWAY_NAME: Dedicated KonnectEventGateway name.
#   TEST_NAME: Value of the gateway's test label.
#   KONNECT_TOKEN, KONNECT_URL: Konnect API token and host.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"

# Mark all dedicated CRs for deletion before remote fallback cleanup, so their
# controllers cannot recreate entities removed below.
kubectl delete eventgatewayvirtualclusterpolicychains.configuration.konghq.com chain -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewayvirtualclusterpolicies.configuration.konghq.com first second -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewayvirtualclusters.configuration.konghq.com chain-vc -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewaybackendclusters.configuration.konghq.com chain-backend -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}" -n "${NAMESPACE}" --ignore-not-found --wait=false

export GATEWAY_NAME TEST_NAME
if ! GATEWAYS="$(API_PATH=/v1/event-gateways bash "${COMMON}/konnect_list_all_entities.sh")"; then
  echo "${GATEWAYS}" >&2
  exit 1
fi
IDS="$(jq -r '
  .body.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME) | .id
' <<<"${GATEWAYS}")"

delete_entity() {
  METHOD=DELETE API_PATH="$1" EXPECTED_STATUS=200,204,404 RETRY_COUNT=30 \
    bash "${COMMON}/konnect_api_request.sh"
}

list_collection() {
  local collection="$1" response existence
  if response="$(API_PATH="${collection}" RETRY_COUNT=1 bash "${COMMON}/konnect_list_all_entities.sh")"; then
    echo "${response}"
  else
    # A concurrently deleted parent makes listing return 404. Do not turn any
    # other API, pagination, or malformed-response failure into an empty set.
    if ! existence="$(API_PATH="${collection}" EXPECTED_STATUS=200,404 RETRY_COUNT=1 \
      bash "${COMMON}/konnect_api_request.sh")"; then
      echo "${existence}" >&2
      return 1
    fi
    if [[ "$(jq -r '.status' <<<"${existence}")" == 404 ]]; then
      echo '{"body":{"data":[]}}'
    else
      echo "${response}" >&2
      return 1
    fi
  fi
}

delete_collection() {
  local collection="$1" response ids id
  response="$(list_collection "${collection}")"
  ids="$(jq -r '.body.data[].id' <<<"${response}")"
  for id in ${ids}; do
    delete_entity "${collection}/${id}"
  done
}

for ID in ${IDS}; do
  GATEWAY_PATH="/v1/event-gateways/${ID}"
  VCS="$(list_collection "${GATEWAY_PATH}/virtual-clusters")"
  VC_IDS="$(jq -r '.body.data[].id' <<<"${VCS}")"
  for VC_ID in ${VC_IDS}; do
    delete_collection "${GATEWAY_PATH}/virtual-clusters/${VC_ID}/cluster-policies"
    delete_entity "${GATEWAY_PATH}/virtual-clusters/${VC_ID}"
  done
  delete_collection "${GATEWAY_PATH}/backend-clusters"
  delete_entity "${GATEWAY_PATH}"
done

API_PATH=/v1/event-gateways EXPECTED_JQ='[.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME)] | length == 0' \
  bash "${COMMON}/konnect_list_all_entities.sh"
