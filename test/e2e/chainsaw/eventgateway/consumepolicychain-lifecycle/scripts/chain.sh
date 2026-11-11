#!/bin/bash
# Keep baselines in one process; only three phases observe a whole sync window.
# shellcheck disable=SC2016 # jq reads exported values.
set -o errexit
set -o nounset
set -o pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=helpers.sh
source "${SCRIPT_DIR}/helpers.sh"

create_policy root modifyHeaders; wait_policy root
create_policy parse-b schemaValidation; wait_policy parse-b
create_policy skip skipRecord parse-b; wait_policy skip
create_policy parse-a schemaValidation; wait_policy parse-a
create_policy first modifyHeaders parse-a; wait_policy first
create_policy second modifyHeaders parse-a; wait_policy second
create_policy decrypted decryptFields parse-a; wait_policy decrypted
ORDER='["parse-a","decrypted","second","first","root","parse-b","skip"]'
create_chain chain "${ORDER}"
BASELINE="$(check "${ORDER}" ready)"
echo "Nested Consume bootstrap: complete flattened order and every parent ID confirmed."

for scope in cluster produce; do
  if [[ "${scope}" == cluster ]]; then
    kind=EventGatewayVirtualClusterPolicyChain
    resource=eventgatewayvirtualclusterpolicychains.configuration.konghq.com
  else
    kind=EventGatewayVirtualClusterProducePolicyChain
    resource=eventgatewayvirtualclusterproducepolicychains.configuration.konghq.com
  fi
  create_chain "${scope}-chain" '[]' "${kind}"
  wait_resource "${resource}" "${scope}-chain" \
    '.metadata.generation as $g | any(.status.conditions[]?; .type == "Programmed" and .status == "True" and .observedGeneration == $g)'
done
CLAIMS="$(kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -ce '.metadata.annotations | with_entries(select(.key |
    . == "gateway-operator.konghq.com/cluster-policy-chain-owner" or
    . == "gateway-operator.konghq.com/produce-policy-chain-owner")) | select(length == 2)')"
VIRTUAL_CLUSTER_NAME=empty-vc create_chain empty-chain '[]'
CHAIN_NAME=empty-chain VIRTUAL_CLUSTER_NAME=empty-vc check '[]' ready >/dev/null
delete_resource "${CHAIN}" empty-chain

ORDER='["parse-b","skip","root","parse-a","first","second","decrypted"]'
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
echo "Whole-group/sibling movement and repaired drift stable beyond the actual sync period."

for invalid in \
  '["first","parse-a","second","decrypted","root","parse-b","skip"]' \
  '["parse-a","first","root","second","decrypted","parse-b","skip"]' \
  '["parse-a","first","parse-b","second","decrypted","skip","root"]'; do
  set_order "${invalid}"
  check "${ORDER}" reference ReferenceInvalid >/dev/null
done
set_order '["parse-a","absent"]'
check "${ORDER}" reference ReferenceNotFound >/dev/null
set_order "${ORDER}"
check "${ORDER}" ready >/dev/null

# A duplicate remote name produces a real unprogrammed reference, not a status patch.
create_policy pending modifyHeaders "" root
wait_resource "${POLICY}" pending \
  '((.status.id // "") == "") and any(.status.conditions[]?; .type == "Programmed" and .status == "False")'
set_order '["pending","parse-b","skip","root","parse-a","first","second","decrypted"]'
CURRENT="$(check "${ORDER}" reference ReferenceNotProgrammed)"
hold "${CURRENT}" reference ReferenceNotProgrammed
kubectl patch "${POLICY}" pending -n "${NAMESPACE}" --type=merge \
  -p '{"spec":{"apiSpec":{"modifyHeaders":{"name":"pending"}}}}' >/dev/null
wait_policy pending
ORDER='["pending","parse-b","skip","root","parse-a","first","second","decrypted"]'
check "${ORDER}" ready >/dev/null
echo "Invalid grouping and missing/unready references retained the complete remote order."

ORIGINAL="$(snapshot "${ORDER}")"
delete_resource "${POLICY}" first
API_PATH="$(jq -r '.policies.first.path' <<<"${ORIGINAL}")" EXPECTED_STATUS=404 RETRY_COUNT=30 \
  quiet bash "${COMMON}/konnect_api_request.sh"
check '["pending","parse-b","skip","root","parse-a","second","decrypted"]' reference ReferenceNotFound >/dev/null
create_policy first modifyHeaders parse-a
wait_policy first
CURRENT="$(check "${ORDER}" ready)"
jq -en --argjson old "${ORIGINAL}" --argjson current "${CURRENT}" '
  $old.policies.first.id != $current.policies.first.id and
  $old.policies.first.uid != $current.policies.first.uid and
  ($old.policies | del(.first)) == ($current.policies | del(.first)) and $old.vc == $current.vc
' >/dev/null || { fail "recreation did not replace only the child's ID and UID"; exit 1; }

create_policy extra modifyHeaders; wait_policy extra
EXTRA_ORDER='["pending","parse-b","skip","root","parse-a","first","second","decrypted","extra"]'
check "${EXTRA_ORDER}" mismatch >/dev/null
set_order "${EXTRA_ORDER}"
check "${EXTRA_ORDER}" ready >/dev/null
set_order "${ORDER}"
CURRENT="$(check "${EXTRA_ORDER}" mismatch)"
hold "${CURRENT}" mismatch
delete_resource "${POLICY}" extra
check "${ORDER}" ready >/dev/null
echo "Real child recreation and full-set addition/omission recovery confirmed."

HANDOVER='["root","pending","parse-a","decrypted","second","first","parse-b","skip"]'
create_chain contender "${HANDOVER}"
CHAIN_NAME=contender check "${ORDER}" conflict >/dev/null
delete_resource "${CHAIN}" chain
CHAIN_NAME=contender
ORDER="${HANDOVER}"
CURRENT="$(check "${ORDER}" ready)"
uid="$(kubectl get "${CHAIN}" contender -n "${NAMESPACE}" -o jsonpath='{.metadata.uid}')"
kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -e --arg claim "contender/${uid}" --argjson claims "${CLAIMS}" '
    .metadata.annotations as $a |
    $a["gateway-operator.konghq.com/consume-policy-chain-owner"] == $claim and
    all($claims | keys[]; . as $k | $a[$k] == $claims[$k])
  ' >/dev/null || { fail "independent claims changed during handover"; exit 1; }

set_order '["absent"]'
check "${ORDER}" reference ReferenceNotFound >/dev/null
trap restore_auth_secret EXIT
trap 'exit 1' HUP INT TERM
kubectl patch secret "${AUTH_SECRET_NAME}" -n "${NAMESPACE}" --type=merge -p '{"data":{"token":null}}' >/dev/null
CONDITION_TYPE=APIAuthValid wait_resource konnectapiauthconfigurations.konnect.konghq.com "${AUTH_NAME}" \
  '.metadata.generation as $g | any(.status.conditions[]?; .type == "APIAuthValid" and .status == "False" and .observedGeneration == $g)'
delete_resource "${CHAIN}" contender
remote_order "${CURRENT}"
assert_identities "${CURRENT}"
kubectl get "${VC}" "${VIRTUAL_CLUSTER_NAME}" -n "${NAMESPACE}" -o json |
  jq -e --argjson claims "${CLAIMS}" '.metadata.annotations as $a |
    $a["gateway-operator.konghq.com/consume-policy-chain-owner"] == null and
    all($claims | keys[]; . as $k | $a[$k] == $claims[$k])
  ' >/dev/null || { fail "retained deletion released the wrong scope"; exit 1; }
jq -cn --argjson snapshot "${CURRENT}" --argjson window "${WINDOW_SECONDS}" --argjson sync "${SYNC_PERIOD_SECONDS}" \
  '{retained: $snapshot, observation_seconds: $window, configured_sync_seconds: $sync}'
echo "Nested Consume management-plane lifecycle passed; authentication restoration follows."
