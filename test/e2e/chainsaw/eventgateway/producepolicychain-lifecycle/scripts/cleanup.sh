#!/bin/bash
# Only the exact-name, test-labeled gateway and this namespace's named CRs are owned.
set -o errexit
set -o nounset
set -o pipefail

test -n "${KONNECT_TOKEN:?KONNECT_TOKEN is required for cleanup}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
export GATEWAY_NAME TEST_NAME

# Capture the allowed remote scope before local deletion can remove its status.
GATEWAYS="$(API_PATH=/v1/event-gateways bash "${COMMON}/konnect_list_all_entities.sh")"
IDS="$(jq -r '.body.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME) | .id' <<<"${GATEWAYS}")"
jq -n '
  {apiVersion: "v1", kind: "Secret",
   metadata: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE,
              labels: {"konghq.com/secret": "true", "konghq.com/credential": "konnect"}},
   type: "Opaque", stringData: {token: env.KONNECT_TOKEN}}
' | kubectl apply -f - >/dev/null

delete_local() {
  kubectl delete "$@" -n "${NAMESPACE}" --ignore-not-found --wait=false >/dev/null
}
delete_local eventgatewayvirtualclusterproducepolicychains.configuration.konghq.com chain contender empty-chain
delete_local eventgatewayvirtualclusterpolicychains.configuration.konghq.com cluster-chain
delete_local eventgatewayvirtualclusterproducepolicies.configuration.konghq.com encrypted first second child-b pending extra root parse-a parse-b
delete_local eventgatewaystatickeys.configuration.konghq.com chain-key
delete_local eventgatewayvirtualclusters.configuration.konghq.com chain-vc empty-vc
delete_local eventgatewaybackendclusters.configuration.konghq.com chain-backend
delete_local konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}"

delete_remote() {
  METHOD=DELETE API_PATH="$1" EXPECTED_STATUS=200,204,404 RETRY_COUNT=15 \
    bash "${COMMON}/konnect_api_request.sh" >/dev/null
}
list_collection() {
  local path="$1" response
  response="$(API_PATH="${path}" EXPECTED_STATUS=200,404 RETRY_COUNT=3 bash "${COMMON}/konnect_api_request.sh")"
  if [[ "$(jq -r '.status' <<<"${response}")" == 404 ]]; then
    echo '{"body":{"data":[]}}'
  else
    API_PATH="${path}" bash "${COMMON}/konnect_list_all_entities.sh"
  fi
}
delete_collection() {
  local path="$1" result id
  result="$(list_collection "${path}")"
  while IFS= read -r id; do
    delete_remote "${path}/${id}"
  done < <(jq -r '.body.data[].id' <<<"${result}")
}
for id in ${IDS}; do
  gateway="/v1/event-gateways/${id}"
  vcs="$(list_collection "${gateway}/virtual-clusters")"
  while IFS= read -r vc_id; do
    path="${gateway}/virtual-clusters/${vc_id}"
    policies="$(list_collection "${path}/produce-policies")"
    # Delete children before schema-validation parents, including after a partial run.
    while IFS= read -r policy_id; do
      delete_remote "${path}/produce-policies/${policy_id}"
    done < <(jq -r '.body.data | sort_by(if .parent_policy_id == null then 1 else 0 end) | .[].id' <<<"${policies}")
    delete_collection "${path}/cluster-policies"
    delete_remote "${path}"
  done < <(jq -r '.body.data[].id' <<<"${vcs}")
  delete_collection "${gateway}/static-keys"
  delete_collection "${gateway}/backend-clusters"
  delete_remote "${gateway}"
  API_PATH="${gateway}" EXPECTED_STATUS=404 RETRY_COUNT=15 bash "${COMMON}/konnect_api_request.sh" >/dev/null
  echo "Confirmed remote 404: ${gateway}"
done
API_PATH=/v1/event-gateways \
  EXPECTED_JQ='[.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME)] | length == 0' \
  bash "${COMMON}/konnect_list_all_entities.sh" >/dev/null

# Ancestor deletion can strand children. Release only deletion finalizers, and
# only after the dedicated remote gateway is independently confirmed absent.
finish_local() {
  local resource="$1" name object
  shift
  for name in "$@"; do
    object="$(kubectl get "${resource}" "${name}" -n "${NAMESPACE}" --ignore-not-found -o json)"
    if [[ -n "${object}" ]]; then
      jq -e '.metadata.deletionTimestamp != null' <<<"${object}" >/dev/null
      kubectl patch "${resource}" "${name}" -n "${NAMESPACE}" --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null
    fi
    test -z "$(kubectl get "${resource}" "${name}" -n "${NAMESPACE}" --ignore-not-found -o name)"
  done
}
finish_local eventgatewayvirtualclusterproducepolicychains.configuration.konghq.com chain contender empty-chain
finish_local eventgatewayvirtualclusterpolicychains.configuration.konghq.com cluster-chain
finish_local eventgatewayvirtualclusterproducepolicies.configuration.konghq.com encrypted first second child-b pending extra root parse-a parse-b
finish_local eventgatewaystatickeys.configuration.konghq.com chain-key
finish_local eventgatewayvirtualclusters.configuration.konghq.com chain-vc empty-vc
finish_local eventgatewaybackendclusters.configuration.konghq.com chain-backend
finish_local konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}"
delete_local konnectapiauthconfigurations.konnect.konghq.com "${AUTH_NAME}"
kubectl wait --for=delete "konnectapiauthconfigurations.konnect.konghq.com/${AUTH_NAME}" -n "${NAMESPACE}" --timeout=30s >/dev/null
delete_local secret chain-key "${AUTH_SECRET_NAME}"
kubectl wait --for=delete secret/chain-key "secret/${AUTH_SECRET_NAME}" -n "${NAMESPACE}" --timeout=30s >/dev/null
test -z "$(kubectl get secret chain-key "${AUTH_SECRET_NAME}" -n "${NAMESPACE}" --ignore-not-found -o name)"
echo "Dedicated gateway absent remotely; all named test resources absent locally."
