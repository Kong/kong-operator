#!/bin/bash
# Keep all baselines in this process: Chainsaw outputs are scoped to one step.
# shellcheck disable=SC2016 # jq reads exported values, not shell interpolation.
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=auth.sh
source "${SCRIPT_DIR}/auth.sh"
POLICY=eventgatewayvirtualclusterproducepolicies.configuration.konghq.com
CHAIN=eventgatewayvirtualclusterproducepolicychains.configuration.konghq.com
VC=eventgatewayvirtualclusters.configuration.konghq.com
CHAIN_NAME=chain
WINDOW_SECONDS="${WINDOW_SECONDS:-65}"
SYNC_PERIOD_SECONDS="${KONNECT_SYNC_PERIOD_SECONDS:-60}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5}"
export CHAIN_NAME

fail() { echo "$1" >&2; exit 1; }
quiet() {
  local output status
  if output="$("$@")"; then
    return 0
  else
    status="$?"
    echo "${output}" >&2
    return "${status}"
  fi
}
for value in "${WINDOW_SECONDS}" "${SYNC_PERIOD_SECONDS}" "${CHECK_INTERVAL}"; do
  [[ "${value}" =~ ^[1-9][0-9]*$ ]] || fail "observation timings must be positive integer seconds"
done
[[ "${WINDOW_SECONDS}" -gt "${SYNC_PERIOD_SECONDS}" ]] || fail "observation window must exceed the actual operator sync period"

wait_resource() {
  RESOURCE_TYPE="$1" RESOURCE_NAME="$2" JQ_CONDITION="$3" \
    quiet bash "${COMMON}/wait_for_resource_reconciled.sh"
}
wait_policy() {
  wait_resource "${POLICY}" "$1" '.metadata.generation as $g |
    .metadata.deletionTimestamp == null and ((.status.id // "") != "") and
    any(.status.conditions[]?; .type == "Programmed" and .status == "True" and .observedGeneration == $g)'
}
delete_resource() {
  kubectl delete "$1" "$2" -n "${NAMESPACE}" --wait=false >/dev/null
  kubectl wait --for=delete "$1/$2" -n "${NAMESPACE}" --timeout=120s >/dev/null
}
create_policy() {
  local name="$1" type="$2" parent="${3:-}" remote_name="${4:-$1}"
  jq -n --arg name "${name}" --arg type "${type}" --arg parent "${parent}" --arg remote "${remote_name}" \
    --arg ns "${NAMESPACE}" --arg vc "${VIRTUAL_CLUSTER_NAME}" '
    (if $type == "schemaValidation" then
       {name: $remote, config: {type: "json", json: {valueValidationAction: "reject"}}}
     elif $type == "encryptFields" then
       {name: $remote, config: {failureMode: "reject", encryptFields: [{
         paths: {type: "array", array: [{match: "$.secret"}]},
         encryptionKey: {type: "static", static: {key: {namespacedRef: {name: "chain-key"}}}}}]}}
     else {name: $remote, config: {actions: [{op: "set", set: {key: "x-chain", value: $name}}]}} end) as $config |
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayVirtualClusterProducePolicy",
     metadata: {name: $name, namespace: $ns},
     spec: {eventGatewayVirtualClusterRef: {type: "namespacedRef", namespacedRef: {name: $vc}},
       apiSpec: ({type: $type, ($type): $config} +
         if $parent == "" then {} else {parentPolicyRef: {name: $parent}} end)}}
  ' | kubectl apply -f - >/dev/null
}
create_chain() {
  jq -n --arg name "$1" --arg ns "${NAMESPACE}" --arg vc "${VIRTUAL_CLUSTER_NAME}" --argjson names "$2" '
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayVirtualClusterProducePolicyChain",
     metadata: {name: $name, namespace: $ns},
     spec: {eventGatewayVirtualClusterRef: {type: "namespacedRef", namespacedRef: {name: $vc}},
            apiSpec: {policies: [$names[] | {name: .}]}}}
  ' | kubectl apply -f - >/dev/null
}
set_order() {
  local patch
  patch="$(jq -cn --argjson names "$1" '{spec: {apiSpec: {policies: [$names[] | {name: .}]}}}')"
  kubectl patch "${CHAIN}" "${CHAIN_NAME}" -n "${NAMESPACE}" --type=merge -p "${patch}" >/dev/null
}
snapshot() {
  local names="$1" programmed="${2:-true}" name object policies='{}' parent
  parent="$(RESOURCE_TYPE="${VC}" RESOURCE_NAME="${VIRTUAL_CLUSTER_NAME}" \
    COLLECTION_PATH_JQ='"/v1/event-gateways/" + .status.gatewayID.id + "/virtual-clusters"' \
    bash "${COMMON}/konnect_entity_of_resource.sh")" || { echo "${parent}" >&2; return 1; }
  while IFS= read -r name; do
    if [[ "${programmed}" == true ]]; then wait_policy "${name}" || return "$?"; fi
    object="$(kubectl get "${POLICY}" "${name}" -n "${NAMESPACE}" -o json)" || return "$?"
    policies="$(jq -cen --argjson all "${policies}" --arg name "${name}" --argjson obj "${object}" --argjson vc "${parent}" '
      select($obj.metadata.deletionTimestamp == null and
        ($obj.status.id | type == "string" and length > 0) and
        $obj.status.virtualClusterID.id == $vc.id) |
      $all + {($name): {id: $obj.status.id, uid: $obj.metadata.uid,
        parent: ($obj.spec.apiSpec.parentPolicyRef.name // ""), type: $obj.spec.apiSpec.type,
        path: ($vc.entity_path + "/produce-policies/" + $obj.status.id)}}
    ')" || fail "invalid policy identity for ${name}"
  done < <(jq -r '.[]' <<<"${names}")
  jq -cen --argjson vc "${parent}" --argjson policies "${policies}" --argjson names "${names}" '
    {vc: $vc, policies: $policies, order: [$names[] | $policies[.].id],
     path: ($vc.entity_path + "/produce-policy-chain")} |
    select((.order | length) == (.order | unique | length))'
}
remote_order() {
  local snap="$1"
  API_PATH="$(jq -r '.path' <<<"${snap}")" EXPECTED_IDS="$(jq -c '.order' <<<"${snap}")" \
    EXPECTED_JQ='type == "object" and (.policies | type == "array") and .policies == ($ENV.EXPECTED_IDS | fromjson)' \
    RETRY_COUNT="${2:-120}" quiet bash "${COMMON}/konnect_api_request.sh"
}
assert_identities() {
  local snap="$1" current name entry parent_id
  current="$(snapshot "$(jq -c '.policies | keys' <<<"${snap}")" false)" || return "$?"
  jq -en --argjson old "${snap}" --argjson current "${current}" \
    '$old.vc == $current.vc and $old.policies == $current.policies' >/dev/null ||
    fail "policy/parent IDs, UIDs, or parentPolicyRef changed"
  while IFS= read -r name; do
    entry="$(jq -c --arg name "${name}" '.policies[$name]' <<<"${snap}")"
    parent_id="$(jq -r --argjson entry "${entry}" '.policies[$entry.parent].id // ""' <<<"${snap}")"
    API_PATH="$(jq -r '.path' <<<"${entry}")" WANT_ID="$(jq -r '.id' <<<"${entry}")" \
      WANT_PARENT="${parent_id}" WANT_TYPE="$(jq -r '.type' <<<"${entry}" | sed -e 's/schemaValidation/schema_validation/' -e 's/modifyHeaders/modify_headers/' -e 's/encryptFields/encrypt_fields/')" \
      EXPECTED_JQ='.id == $ENV.WANT_ID and .type == $ENV.WANT_TYPE and
        ((.parent_policy_id // "") == $ENV.WANT_PARENT)' RETRY_COUNT=1 \
      quiet bash "${COMMON}/konnect_api_request.sh" || return "$?"
  done < <(jq -r '.policies | keys[]' <<<"${snap}")
}
assert_state() {
  local state="$1" reason="${2:-}" condition
  export EXPECTED_REASON="${reason}"
  condition='.metadata.generation as $g | .status.conditions as $conditions |
    ((.status.id // "") == "") and
    any($conditions[]?; .type == "Programmed" and .observedGeneration == $g and .status == "False")'
  case "${state}" in
    ready)
      condition='.metadata.generation as $g | .status.conditions as $conditions |
        ((.status.id // "") == "") and
        all(["EventGatewayVirtualClusterRefValid", "APIAuthResolvedRef", "APIAuthValid",
             "KonnectReferencesResolved", "PolicyChainOwnerValid", "Programmed"][];
          . as $type | any($conditions[]?; .type == $type and .status == "True" and .observedGeneration == $g))'
      ;;
    reference)
      condition+=' and any($conditions[]?; .type == "KonnectReferencesResolved" and
        .status == "False" and .observedGeneration == $g and .reason == $ENV.EXPECTED_REASON)'
      ;;
    mismatch)
      condition+=' and any($conditions[]?; .type == "KonnectReferencesResolved" and
        .status == "True" and .observedGeneration == $g) and
        any($conditions[]?; .type == "Programmed" and (.message | contains("complete remote produce policy chain")))'
      ;;
    conflict)
      condition+=' and any($conditions[]?; .type == "PolicyChainOwnerValid" and
        .status == "False" and .observedGeneration == $g and .reason == "ConflictingChain")'
      ;;
    *) fail "unknown chain state ${state}" ;;
  esac
  wait_resource "${CHAIN}" "${CHAIN_NAME}" "${condition}"
}
check() {
  local names="$1" state="$2" reason="${3:-}" snap
  assert_state "${state}" "${reason}" || return "$?"
  snap="$(snapshot "${names}")" || return "$?"
  remote_order "${snap}" || return "$?"
  assert_identities "${snap}" || return "$?"
  echo "${snap}"
}
hold() {
  local snap="$1" state="$2" reason="${3:-}" start="${SECONDS}"
  while true; do
    assert_state "${state}" "${reason}"
    remote_order "${snap}" 1
    assert_identities "${snap}"
    [[ $((SECONDS - start)) -ge "${WINDOW_SECONDS}" ]] && break
    sleep "${CHECK_INTERVAL}"
  done
}

# Allow the helper regression tests to exercise the real functions without provisioning.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return; fi

# Public test-only key material, not the Konnect credential.
jq -n --arg ns "${NAMESPACE}" --arg gateway "${GATEWAY_NAME}" '
  {apiVersion: "v1", kind: "Secret", metadata: {name: "chain-key", namespace: $ns},
   stringData: {key: "Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy1vayE="}},
  {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayStaticKey",
   metadata: {name: "chain-key", namespace: $ns},
   spec: {gatewayRef: {type: "namespacedRef", namespacedRef: {name: $gateway}},
     apiSpec: {name: "chain-key", value: {type: "secretRef", secretRef: {name: "chain-key", key: "key"}}}}}
' | kubectl apply -f - >/dev/null
wait_resource eventgatewaystatickeys.configuration.konghq.com chain-key \
  '.metadata.generation as $g | any(.status.conditions[]?; .type == "Programmed" and .status == "True" and .observedGeneration == $g)'

create_policy root modifyHeaders; wait_policy root
create_policy parse-b schemaValidation; wait_policy parse-b
create_policy child-b modifyHeaders parse-b; wait_policy child-b
create_policy parse-a schemaValidation; wait_policy parse-a
create_policy first modifyHeaders parse-a; wait_policy first
create_policy second modifyHeaders parse-a; wait_policy second
create_policy encrypted encryptFields parse-a; wait_policy encrypted
ORDER='["parse-a","encrypted","second","first","root","parse-b","child-b"]'
create_chain chain "${ORDER}"
BASELINE="$(check "${ORDER}" ready)"
echo "Nested bootstrap: exact complete order and parent relationships confirmed."
key_id="$(kubectl get eventgatewaystatickeys.configuration.konghq.com chain-key -n "${NAMESPACE}" -o jsonpath='{.status.id}')"
API_PATH="$(jq -r '.policies.encrypted.path' <<<"${BASELINE}")" KEY_ID="${key_id}" \
  EXPECTED_JQ='.config.encrypt_fields[0].encryption_key.key.id == $ENV.KEY_ID' \
  quiet bash "${COMMON}/konnect_api_request.sh"

# Independent cluster-level singleton on the same VC.
jq -n --arg ns "${NAMESPACE}" --arg vc "${VIRTUAL_CLUSTER_NAME}" '
  {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayVirtualClusterPolicyChain",
   metadata: {name: "cluster-chain", namespace: $ns},
   spec: {eventGatewayVirtualClusterRef: {type: "namespacedRef", namespacedRef: {name: $vc}}, apiSpec: {policies: []}}}
' | kubectl apply -f - >/dev/null
wait_resource eventgatewayvirtualclusterpolicychains.configuration.konghq.com cluster-chain \
  '.metadata.generation as $g | any(.status.conditions[]?; .type == "Programmed" and .status == "True" and .observedGeneration == $g)'
cluster_claim="$(kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -er '.metadata.annotations["gateway-operator.konghq.com/cluster-policy-chain-owner"]')"

VIRTUAL_CLUSTER_NAME=empty-vc create_chain empty-chain '[]'
CHAIN_NAME=empty-chain VIRTUAL_CLUSTER_NAME=empty-vc check '[]' ready >/dev/null
delete_resource "${CHAIN}" empty-chain

# Move whole groups and reorder siblings without changing parent relationships.
ORDER='["parse-b","child-b","root","parse-a","first","second","encrypted"]'
set_order "${ORDER}"
CURRENT="$(check "${ORDER}" ready)"
assert_identities "${BASELINE}"
export API_PATH BODY DRIFT_IDS
API_PATH="$(jq -r '.path' <<<"${CURRENT}")"
DRIFT_IDS="$(jq -c '.order' <<<"${BASELINE}")"
BODY='{policies: ($ENV.DRIFT_IDS | fromjson)}'
METHOD=PUT EXPECTED_JQ='.policies == ($ENV.DRIFT_IDS | fromjson)' \
  quiet bash "${COMMON}/konnect_api_request.sh"
unset BODY
CURRENT="$(check "${ORDER}" ready)"
hold "${CURRENT}" ready
echo "Group/sibling reorder and drift repair stable beyond the actual sync period."

for invalid in \
  '["parse-a","first","root","second","encrypted","parse-b","child-b"]' \
  '["first","parse-a","second","encrypted","root","parse-b","child-b"]' \
  '["parse-a","first","parse-b","second","encrypted","child-b","root"]'; do
  set_order "${invalid}"
  check "${ORDER}" reference ReferenceInvalid >/dev/null
done
set_order '["parse-a","absent"]'
check "${ORDER}" reference ReferenceNotFound >/dev/null
set_order "${ORDER}"
check "${ORDER}" ready >/dev/null

# Duplicate remote names cannot be adopted by a newly unprogrammed CR.
create_policy pending modifyHeaders "" root
wait_resource "${POLICY}" pending \
  '((.status.id // "") == "") and any(.status.conditions[]?; .type == "Programmed" and .status == "False")'
set_order '["pending","parse-b","child-b","root","parse-a","first","second","encrypted"]'
CURRENT="$(check "${ORDER}" reference ReferenceNotProgrammed)"
hold "${CURRENT}" reference ReferenceNotProgrammed
echo "Invalid grouping and absent/unready references blocked without shortening the remote order."
kubectl patch "${POLICY}" pending -n "${NAMESPACE}" --type=merge \
  -p '{"spec":{"apiSpec":{"modifyHeaders":{"name":"pending"}}}}' >/dev/null
wait_policy pending
ORDER='["pending","parse-b","child-b","root","parse-a","first","second","encrypted"]'
check "${ORDER}" ready >/dev/null

ORIGINAL="$(snapshot "${ORDER}")"
delete_resource "${POLICY}" first
API_PATH="$(jq -r '.policies.first.path' <<<"${ORIGINAL}")" EXPECTED_STATUS=404 RETRY_COUNT=30 \
  quiet bash "${COMMON}/konnect_api_request.sh"
check '["pending","parse-b","child-b","root","parse-a","second","encrypted"]' reference ReferenceNotFound >/dev/null
create_policy first modifyHeaders parse-a
wait_policy first
CURRENT="$(check "${ORDER}" ready)"
jq -en --argjson old "${ORIGINAL}" --argjson current "${CURRENT}" '
  $old.policies.first.id != $current.policies.first.id and
  $old.policies.first.uid != $current.policies.first.uid and
  ($old.policies | del(.first)) == ($current.policies | del(.first)) and $old.vc == $current.vc
' >/dev/null || fail "recreation did not replace only first with its latest ID and UID"
echo "Real child recreation recovered with a new Konnect ID and Kubernetes UID."

create_policy extra modifyHeaders; wait_policy extra
EXTRA_ORDER='["pending","parse-b","child-b","root","parse-a","first","second","encrypted","extra"]'
check "${EXTRA_ORDER}" mismatch >/dev/null
set_order "${EXTRA_ORDER}"
check "${EXTRA_ORDER}" ready >/dev/null
set_order "${ORDER}"
CURRENT="$(check "${EXTRA_ORDER}" mismatch)"
# Observe a blocked complete-set mismatch beyond the actual default sync period.
hold "${CURRENT}" mismatch
echo "Extra/omitted complete-set mismatch blocked beyond the actual sync period."
delete_resource "${POLICY}" extra
check "${ORDER}" ready >/dev/null

create_chain contender '["root","pending","parse-a","encrypted","second","first","parse-b","child-b"]'
CHAIN_NAME=contender check "${ORDER}" conflict >/dev/null
delete_resource "${CHAIN}" chain
CHAIN_NAME=contender
ORDER='["root","pending","parse-a","encrypted","second","first","parse-b","child-b"]'
CURRENT="$(check "${ORDER}" ready)"
uid="$(kubectl get "${CHAIN}" contender -n "${NAMESPACE}" -o jsonpath='{.metadata.uid}')"
kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -e --arg claim "contender/${uid}" --arg cluster "${cluster_claim}" '
    .metadata.annotations["gateway-operator.konghq.com/produce-policy-chain-owner"] == $claim and
    .metadata.annotations["gateway-operator.konghq.com/cluster-policy-chain-owner"] == $cluster
  ' >/dev/null || fail "independent ownership was not preserved during handover"

set_order '["absent"]'
check "${ORDER}" reference ReferenceNotFound >/dev/null
trap restore_auth_secret EXIT
trap 'exit 1' HUP INT TERM
kubectl patch secret "${AUTH_SECRET_NAME}" -n "${NAMESPACE}" --type=merge -p '{"data":{"token":null}}' >/dev/null
CONDITION_TYPE=APIAuthValid wait_resource konnectapiauthconfigurations.konnect.konghq.com "${AUTH_NAME}" \
  '.metadata.generation as $g | any(.status.conditions[]?; .type == "APIAuthValid" and .status == "False" and .observedGeneration == $g)'
delete_resource "${CHAIN}" contender
remote_order "${CURRENT}" 1
assert_identities "${CURRENT}"
kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -e --arg cluster "${cluster_claim}" '
    .metadata.annotations["gateway-operator.konghq.com/produce-policy-chain-owner"] == null and
    .metadata.annotations["gateway-operator.konghq.com/cluster-policy-chain-owner"] == $cluster
  ' >/dev/null || fail "retained deletion released the wrong ownership scope"
jq -cn --argjson snapshot "${CURRENT}" --argjson window "${WINDOW_SECONDS}" --argjson sync "${SYNC_PERIOD_SECONDS}" \
  '{retained: $snapshot, observation_seconds: $window, configured_sync_seconds: $sync}'
echo "Complete nested Produce policy-chain lifecycle passed; authentication restoration follows."
