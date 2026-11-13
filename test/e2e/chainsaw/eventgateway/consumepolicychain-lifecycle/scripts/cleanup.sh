#!/bin/bash
# Own only named CRs in the test namespace and its exact-name/test-labeled gateway.
# shellcheck disable=SC2016 # jq reads exported values.
set -o errexit
set -o nounset
set -o pipefail
test -n "${KONNECT_TOKEN:?KONNECT_TOKEN is required for cleanup}"
test -n "${NAMESPACE:?NAMESPACE is required}"
test -n "${GATEWAY_NAME:?GATEWAY_NAME is required}"
test -n "${TEST_NAME:?TEST_NAME is required}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
export GATEWAY_NAME TEST_NAME

checked() {
  local output status
  if output="$("$@")"; then
    printf '%s\n' "${output}"
  else
    status="$?"
    echo "${output}" >&2
    return "${status}"
  fi
}
list_collection() {
  local response
  response="$(API_PATH="$1" EXPECTED_STATUS=200,404 RETRY_COUNT=3 \
    bash "${COMMON}/konnect_api_request.sh")" || { echo "${response}" >&2; return 1; }
  if [[ "$(jq -er '.status' <<<"${response}")" == 404 ]]; then
    echo '{"body":{"data":[]}}'
  else
    API_PATH="$1" RETRY_COUNT=3 checked bash "${COMMON}/konnect_list_all_entities.sh"
  fi
}
ids() {
  jq -er '.body.data | if type == "array" and
    all(.[]; .id | type == "string" and test("^[A-Za-z0-9-]+$"))
    then map(.id) | join("\n") else error("invalid collection IDs") end'
}
delete_remote() {
  METHOD=DELETE API_PATH="$1" EXPECTED_STATUS=200,204,404 RETRY_COUNT=3 \
    checked bash "${COMMON}/konnect_api_request.sh" >/dev/null
}
delete_collection() {
  local result list id
  result="$(list_collection "$1")" || return "$?"
  list="$(ids <<<"${result}")" || return "$?"
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    delete_remote "$1/${id}" || return "$?"
  done <<<"${list}"
}
GATEWAYS="$(API_PATH=/v1/event-gateways RETRY_COUNT=3 bash "${COMMON}/konnect_list_all_entities.sh")" ||
  { echo "${GATEWAYS}" >&2; exit 1; }
OWNED="$(jq -ce '{body: {data: [.body.data[] |
  select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME)]}}' <<<"${GATEWAYS}")"
IDS="$(ids <<<"${OWNED}")"
jq -n '
  {apiVersion: "v1", kind: "Secret",
   metadata: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE,
              labels: {"konghq.com/secret": "true", "konghq.com/credential": "konnect"}},
   type: "Opaque", stringData: {token: env.KONNECT_TOKEN}}
' | kubectl apply -f - >/dev/null

local_resources() {
  "$1" eventgatewayvirtualclusterconsumepolicychains.configuration.konghq.com chain contender empty-chain
  "$1" eventgatewayvirtualclusterproducepolicychains.configuration.konghq.com produce-chain
  "$1" eventgatewayvirtualclusterpolicychains.configuration.konghq.com cluster-chain
  "$1" eventgatewayvirtualclusterconsumepolicies.configuration.konghq.com decrypted first second skip pending extra root parse-a parse-b
  "$1" eventgatewayvirtualclusters.configuration.konghq.com chain-vc empty-vc
  "$1" eventgatewaybackendclusters.configuration.konghq.com chain-backend
  "$1" konnecteventgateways.konnect.konghq.com "${GATEWAY_NAME}"
}
delete_local() {
  kubectl delete "$@" -n "${NAMESPACE}" --ignore-not-found --wait=false >/dev/null
}
local_resources delete_local
while IFS= read -r id; do
  [[ -n "${id}" ]] || continue
  gateway="/v1/event-gateways/${id}"
  vcs="$(list_collection "${gateway}/virtual-clusters")"
  vc_ids="$(ids <<<"${vcs}")"
  while IFS= read -r vc_id; do
    [[ -n "${vc_id}" ]] || continue
    path="${gateway}/virtual-clusters/${vc_id}"
    policies="$(list_collection "${path}/consume-policies")"
    children_first="$(jq -ce '.body.data |= sort_by(if .parent_policy_id == null then 1 else 0 end)' <<<"${policies}")"
    policy_ids="$(ids <<<"${children_first}")"
    while IFS= read -r policy_id; do
      [[ -n "${policy_id}" ]] || continue
      delete_remote "${path}/consume-policies/${policy_id}"
    done <<<"${policy_ids}"
    delete_collection "${path}/produce-policies"
    delete_collection "${path}/cluster-policies"
    delete_remote "${path}"
  done <<<"${vc_ids}"
  delete_collection "${gateway}/backend-clusters"
  delete_remote "${gateway}"
  API_PATH="${gateway}" EXPECTED_STATUS=404 RETRY_COUNT=3 \
    checked bash "${COMMON}/konnect_api_request.sh" >/dev/null
done <<<"${IDS}"
API_PATH=/v1/event-gateways RETRY_COUNT=3 \
  EXPECTED_JQ='[.data[] | select(.name == $ENV.GATEWAY_NAME and .labels.test == $ENV.TEST_NAME)] | length == 0' \
  checked bash "${COMMON}/konnect_list_all_entities.sh" >/dev/null

# Only release stranded local finalizers after independently verifying remote absence.
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
local_resources finish_local
delete_local konnectapiauthconfigurations.konnect.konghq.com "${AUTH_NAME}"
kubectl wait --for=delete "konnectapiauthconfigurations.konnect.konghq.com/${AUTH_NAME}" -n "${NAMESPACE}" --timeout=30s >/dev/null
delete_local secret "${AUTH_SECRET_NAME}"
kubectl wait --for=delete "secret/${AUTH_SECRET_NAME}" -n "${NAMESPACE}" --timeout=30s >/dev/null
echo "Exact-name/test-labeled gateway absent remotely; named local resources removed."
