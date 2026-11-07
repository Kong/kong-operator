#!/bin/bash
# Clean only the dedicated gateway's exact-name, test-labeled remote scope.
set -o errexit
set -o nounset
set -o pipefail

test -n "${KONNECT_TOKEN:?KONNECT_TOKEN must be set for isolated remote cleanup}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"

# Restore only this test's auth if a failure interrupted the no-auth scenario.
jq -n '
  {apiVersion: "v1", kind: "Secret",
   metadata: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE,
              labels: {"konghq.com/secret": "true", "konghq.com/credential": "konnect"}},
   type: "Opaque", stringData: {token: env.KONNECT_TOKEN}}
' | kubectl apply -f - >/dev/null
jq -n '
  {apiVersion: "konnect.konghq.com/v1alpha1", kind: "KonnectAPIAuthConfiguration",
   metadata: {name: env.AUTH_NAME, namespace: env.NAMESPACE},
   spec: {type: "secretRef", secretRef: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE},
          serverURL: env.KONNECT_URL}}
' | kubectl apply -f - >/dev/null

# Stop local recreation first, including resources created by lifecycle scripts.
kubectl delete eventgatewaylistenerpolicychains.configuration.konghq.com chain contender empty-chain -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewaylistenerpolicies.configuration.konghq.com first second pending third -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewaylisteners.configuration.konghq.com chain-listener empty-listener -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewayvirtualclusters.configuration.konghq.com chain-vc -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete eventgatewaybackendclusters.configuration.konghq.com chain-backend -n "${NAMESPACE}" --ignore-not-found --wait=false
kubectl delete konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}" -n "${NAMESPACE}" --ignore-not-found --wait=false

export GATEWAY_NAME TEST_NAME
if ! GATEWAYS="$(API_PATH=/v1/event-gateways bash "${COMMON}/konnect_list_all_entities.sh")"; then
  echo "${GATEWAYS}" >&2
  exit 1
fi
IDS="$(jq -r '.body.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME) | .id' <<<"${GATEWAYS}")"

delete_entity() {
  METHOD=DELETE API_PATH="$1" EXPECTED_STATUS=200,204,404 RETRY_COUNT=30 \
    bash "${COMMON}/konnect_api_request.sh" >&2
}

list_collection() {
  local collection="$1" response existence
  if response="$(API_PATH="${collection}" RETRY_COUNT=1 bash "${COMMON}/konnect_list_all_entities.sh")"; then
    echo "${response}"
  else
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
  LISTENERS="$(list_collection "${GATEWAY_PATH}/listeners")"
  LISTENER_IDS="$(jq -r '.body.data[].id' <<<"${LISTENERS}")"
  for LISTENER_ID in ${LISTENER_IDS}; do
    delete_collection "${GATEWAY_PATH}/listeners/${LISTENER_ID}/policies"
    delete_entity "${GATEWAY_PATH}/listeners/${LISTENER_ID}"
  done
  VCS="$(list_collection "${GATEWAY_PATH}/virtual-clusters")"
  VC_IDS="$(jq -r '.body.data[].id' <<<"${VCS}")"
  for VC_ID in ${VC_IDS}; do
    delete_collection "${GATEWAY_PATH}/virtual-clusters/${VC_ID}/cluster-policies"
    delete_entity "${GATEWAY_PATH}/virtual-clusters/${VC_ID}"
  done
  delete_collection "${GATEWAY_PATH}/backend-clusters"
  delete_entity "${GATEWAY_PATH}"
done

# shellcheck disable=SC2016
API_PATH=/v1/event-gateways EXPECTED_JQ='[.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME)] | length == 0' \
  bash "${COMMON}/konnect_list_all_entities.sh" >&2

# Ancestor deletion can strand a child whose auth path no longer resolves.
# Only release cleanup finalizers after the dedicated remote gateway is gone.
finish_deletion() {
  local resource="$1" name object
  shift
  for name in "$@"; do
    object="$(kubectl get "${resource}" "${name}" -n "${NAMESPACE}" --ignore-not-found -o json)"
    if [[ -n "${object}" ]]; then
      jq -e '.metadata.deletionTimestamp != null' <<<"${object}" >/dev/null
      kubectl patch "${resource}" "${name}" -n "${NAMESPACE}" --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null
    fi
  done
}
finish_deletion eventgatewaylistenerpolicychains.configuration.konghq.com chain contender empty-chain
finish_deletion eventgatewaylistenerpolicies.configuration.konghq.com first second pending third
finish_deletion eventgatewaylisteners.configuration.konghq.com chain-listener empty-listener
finish_deletion eventgatewayvirtualclusters.configuration.konghq.com chain-vc
finish_deletion eventgatewaybackendclusters.configuration.konghq.com chain-backend
finish_deletion konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}"
