#!/bin/bash
# shellcheck disable=SC2016 # jq reads exported values.
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
# Reuse the public credential restoration handler, including its exit-status contract.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../../producepolicychain-lifecycle/scripts/auth.sh
source "${SCRIPT_DIR}/../../producepolicychain-lifecycle/scripts/auth.sh"
POLICY=eventgatewayvirtualclusterconsumepolicies.configuration.konghq.com
CHAIN=eventgatewayvirtualclusterconsumepolicychains.configuration.konghq.com
VC=eventgatewayvirtualclusters.configuration.konghq.com
CHAIN_NAME=chain
WINDOW_SECONDS="${WINDOW_SECONDS:-65}"
SYNC_PERIOD_SECONDS="${KONNECT_SYNC_PERIOD_SECONDS:-60}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5}"
export CHAIN_NAME

fail() { echo "$1" >&2; return 1; }
quiet() {
  local output status
  if output="$("$@")"; then return 0; else status="$?"; fi
  echo "${output}" >&2
  return "${status}"
}
for value in "${WINDOW_SECONDS}" "${SYNC_PERIOD_SECONDS}" "${CHECK_INTERVAL}"; do
  [[ "${value}" =~ ^[1-9][0-9]*$ ]] || { fail "observation timings must be positive integer seconds"; exit 1; }
done
[[ "${WINDOW_SECONDS}" -gt "${SYNC_PERIOD_SECONDS}" ]] ||
  { fail "observation window must exceed the actual operator sync period"; exit 1; }

wait_resource() {
  RESOURCE_TYPE="$1" RESOURCE_NAME="$2" JQ_CONDITION="$3" \
    quiet bash "${COMMON}/wait_for_resource_reconciled.sh"
}
wait_policy() {
  wait_resource "${POLICY}" "$1" '.metadata.generation as $g |
    .metadata.deletionTimestamp == null and (.status.id | type == "string" and length > 0) and
    any(.status.conditions[]?; .type == "Programmed" and .status == "True" and .observedGeneration == $g)'
}
delete_resource() {
  kubectl delete "$1" "$2" -n "${NAMESPACE}" --wait=false >/dev/null || return "$?"
  kubectl wait --for=delete "$1/$2" -n "${NAMESPACE}" --timeout=120s >/dev/null
}
create_policy() {
  local name="$1" type="$2" parent="${3:-}" remote_name="${4:-$1}"
  jq -n --arg name "${name}" --arg type "${type}" --arg parent "${parent}" --arg remote "${remote_name}" \
    --arg ns "${NAMESPACE}" --arg vc "${VIRTUAL_CLUSTER_NAME}" --arg test "${TEST_NAME}" '
    (if $type == "schemaValidation" then
       {name: $remote, config: {type: "json", json: {valueValidationAction: "skip"}}}
     elif $type == "decryptFields" then
       {name: $remote, config: {failureMode: "error",
         decryptFields: {paths: {type: "array", array: [{match: "secret"}]}},
         keySources: [{type: "static", static: {}}]}}
     elif $type == "skipRecord" then
       {name: $remote, condition: "record.value.skip == true"}
     elif $type == "modifyHeaders" then
       {name: $remote, config: {actions: [{op: "set", set: {key: "x-consume-chain", value: $name}}]}}
     else error("unsupported Consume fixture type: " + $type) end) as $config |
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayVirtualClusterConsumePolicy",
     metadata: {name: $name, namespace: $ns, labels: {test: $test}},
     spec: {eventGatewayVirtualClusterRef: {type: "namespacedRef", namespacedRef: {name: $vc}},
       apiSpec: ({type: $type, ($type): $config} +
         if $parent == "" then {} else {parentPolicyRef: {name: $parent}} end)}}
  ' | kubectl apply -f - >/dev/null
}
create_chain() {
  local kind="${3:-EventGatewayVirtualClusterConsumePolicyChain}"
  jq -n --arg name "$1" --arg ns "${NAMESPACE}" --arg vc "${VIRTUAL_CLUSTER_NAME}" \
    --arg kind "${kind}" --arg test "${TEST_NAME}" --argjson names "$2" '
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: $kind,
     metadata: {name: $name, namespace: $ns, labels: {test: $test}},
     spec: {eventGatewayVirtualClusterRef: {type: "namespacedRef", namespacedRef: {name: $vc}},
            apiSpec: {policies: [$names[] | {name: .}]}}}
  ' | kubectl apply -f - >/dev/null
}
set_order() {
  local patch
  patch="$(jq -cen --argjson names "$1" '{spec: {apiSpec: {policies: [$names[] | {name: .}]}}}')" || return "$?"
  kubectl patch "${CHAIN}" "${CHAIN_NAME}" -n "${NAMESPACE}" --type=merge -p "${patch}" >/dev/null
}
snapshot() {
  local names="$1" programmed="${2:-true}" name object policies='{}' parent list
  list="$(jq -er 'if type == "array" and all(.[]; type == "string" and length > 0)
    then join("\n") else error("invalid policy names") end' <<<"${names}")" || return "$?"
  parent="$(RESOURCE_TYPE="${VC}" RESOURCE_NAME="${VIRTUAL_CLUSTER_NAME}" \
    COLLECTION_PATH_JQ='"/v1/event-gateways/" + .status.gatewayID.id + "/virtual-clusters"' \
    bash "${COMMON}/konnect_entity_of_resource.sh")" || { echo "${parent}" >&2; return 1; }
  jq -e 'all(.id, .uid, .entity_path; type == "string" and length > 0)' <<<"${parent}" >/dev/null ||
    { fail "invalid virtual cluster identity"; return 1; }
  while IFS= read -r name; do
    [[ -n "${name}" ]] || continue
    if [[ "${programmed}" == true ]]; then wait_policy "${name}" || return "$?"; fi
    object="$(kubectl get "${POLICY}" "${name}" -n "${NAMESPACE}" -o json)" || return "$?"
    policies="$(jq -cen --argjson all "${policies}" --arg name "${name}" --argjson obj "${object}" --argjson vc "${parent}" '
      select($obj.metadata.deletionTimestamp == null and
        ($obj.status.id | type == "string" and length > 0) and
        ($obj.metadata.uid | type == "string" and length > 0) and
        $obj.status.virtualClusterID.id == $vc.id and
        $obj.status.gatewayID.id == ($vc.collection_path | split("/") | .[-2])) |
      $all + {($name): {id: $obj.status.id, uid: $obj.metadata.uid,
        parent: ($obj.spec.apiSpec.parentPolicyRef.name // ""), type: $obj.spec.apiSpec.type,
        path: ($vc.entity_path + "/consume-policies/" + $obj.status.id)}}'
    )" || { fail "invalid policy identity for ${name}"; return 1; }
  done <<<"${list}"
  jq -cen --argjson vc "${parent}" --argjson policies "${policies}" --argjson names "${names}" '
    {vc: $vc, policies: $policies, order: [$names[] | $policies[.].id],
     path: ($vc.entity_path + "/consume-policy-chain")} |
    select((.order | length) == (.order | unique | length))' ||
    { fail "duplicate or invalid policy snapshot"; return 1; }
}
remote_order() {
  API_PATH="$(jq -er '.path' <<<"$1")" EXPECTED_IDS="$(jq -c '.order' <<<"$1")" \
    EXPECTED_JQ='type == "object" and (.policies | type == "array") and .policies == ($ENV.EXPECTED_IDS | fromjson)' \
    RETRY_COUNT="${2:-1}" quiet bash "${COMMON}/konnect_api_request.sh"
}
assert_identities() {
  local snap="$1" current names name entry parent_id remote_type list
  names="$(jq -c '.policies | keys' <<<"${snap}")" || return "$?"
  current="$(snapshot "${names}" false)" || return "$?"
  jq -en --argjson old "${snap}" --argjson current "${current}" \
    '$old.vc == $current.vc and $old.policies == $current.policies' >/dev/null ||
    { fail "policy/parent IDs, UIDs, or parentPolicyRef changed"; return 1; }
  list="$(jq -r '.policies | keys[]' <<<"${snap}")" || return "$?"
  while IFS= read -r name; do
    [[ -n "${name}" ]] || continue
    entry="$(jq -c --arg name "${name}" '.policies[$name]' <<<"${snap}")" || return "$?"
    parent_id="$(jq -er --argjson entry "${entry}" '
      if $entry.parent == "" then "" else .policies[$entry.parent].id |
        select(type == "string" and length > 0) end' <<<"${snap}")" || return "$?"
    remote_type="$(jq -er '.type | {schemaValidation:"schema_validation", modifyHeaders:"modify_headers",
      decryptFields:"decrypt_fields", skipRecord:"skip_record"}[.]' <<<"${entry}")" || return "$?"
    API_PATH="$(jq -r '.path' <<<"${entry}")" WANT_ID="$(jq -r '.id' <<<"${entry}")" \
      WANT_PARENT="${parent_id}" WANT_TYPE="${remote_type}" EXPECTED_JQ='type == "object" and
        .id == $ENV.WANT_ID and .type == $ENV.WANT_TYPE and
        (if $ENV.WANT_PARENT == "" then .parent_policy_id == null
         else .parent_policy_id == $ENV.WANT_PARENT end)' RETRY_COUNT=1 \
      quiet bash "${COMMON}/konnect_api_request.sh" || return "$?"
  done <<<"${list}"
}
assert_state() {
  local state="$1" reason="${2:-}" condition
  export EXPECTED_REASON="${reason}"
  condition='.metadata.generation as $g | .status.conditions as $conditions |
    ((.status.id // "") == "") and
    any($conditions[]?; .type == "Programmed" and .observedGeneration == $g and .status == "False")'
  case "${state}" in
    ready)
      condition='.metadata.generation as $g | .status.conditions as $conditions | ((.status.id // "") == "") and
        all(["EventGatewayVirtualClusterRefValid", "APIAuthResolvedRef", "APIAuthValid",
             "KonnectReferencesResolved", "PolicyChainOwnerValid", "Programmed"][];
          . as $type | any($conditions[]?; .type == $type and .status == "True" and .observedGeneration == $g))' ;;
    reference)
      condition+=' and any($conditions[]?; .type == "KonnectReferencesResolved" and
        .status == "False" and .observedGeneration == $g and .reason == $ENV.EXPECTED_REASON)' ;;
    mismatch)
      condition+=' and any($conditions[]?; .type == "KonnectReferencesResolved" and
        .status == "True" and .observedGeneration == $g) and
        any($conditions[]?; .type == "Programmed" and (.message | contains("complete remote consume policy chain")))' ;;
    conflict)
      condition+=' and any($conditions[]?; .type == "PolicyChainOwnerValid" and
        .status == "False" and .observedGeneration == $g and .reason == "ConflictingChain")' ;;
    *) fail "unknown chain state ${state}"; return 1 ;;
  esac
  wait_resource "${CHAIN}" "${CHAIN_NAME}" "${condition}"
}
check() {
  local names="$1" state="$2" reason="${3:-}" snap
  assert_state "${state}" "${reason}" || return "$?"
  snap="$(snapshot "${names}")" || return "$?"
  remote_order "${snap}" 120 || return "$?"
  MAX_RETRIES=1 assert_identities "${snap}" || return "$?"
  echo "${snap}"
}
hold() {
  local snap="$1" state="$2" reason="${3:-}" start="${SECONDS}"
  while true; do
    MAX_RETRIES=1 assert_state "${state}" "${reason}" || return "$?"
    remote_order "${snap}" || return "$?"
    assert_identities "${snap}" || return "$?"
    [[ $((SECONDS - start)) -ge "${WINDOW_SECONDS}" ]] && break
    sleep "${CHECK_INTERVAL}"
  done
}
