#!/bin/bash
# Exercise complete listener ordering and retain identities across lifecycle changes.
# shellcheck disable=SC2089,SC2090 # Exported jq programs are consumed by shared scripts, not shell words.
set -o errexit
set -o nounset
set -o pipefail

# NAMESPACE, LISTENER_NAME, VIRTUAL_CLUSTER_NAME, AUTH_NAME, AUTH_SECRET_NAME,
# KONNECT_TOKEN and KONNECT_URL are provided by Chainsaw. Snapshots stay in memory.
# HOLD=true is used only after the chain is deleted: nothing reconciles the
# deleted chain, but the listener and the policies still sync (60s by default)
# and could reorder the retained chain.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON="${SCRIPT_DIR}/../../../common/scripts"
MODE="${1:?expected a lifecycle scenario}"
WINDOW_SECONDS="${WINDOW_SECONDS:-75}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5}"
CHAIN_NAME=chain
export CHAIN_NAME

fail() {
  jq -n --arg error "$1" '{error: $error}' >&2
  exit 1
}

for VALUE in "${WINDOW_SECONDS}" "${CHECK_INTERVAL}"; do
  [[ "${VALUE}" =~ ^[1-9][0-9]*$ ]] || fail "observation timings must be positive integer seconds"
done

create_chain() {
  jq -n --arg name "$1" --arg ns "${NAMESPACE}" --arg listener "${LISTENER_NAME}" --argjson names "$2" '
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayListenerPolicyChain",
     metadata: {name: $name, namespace: $ns},
     spec: {eventGatewayListenerRef: {type: "namespacedRef", namespacedRef: {name: $listener}},
            apiSpec: {policies: [$names[] | {name: .}]}}}
  ' | kubectl apply -f - >/dev/null
}

set_order() {
  local patch
  patch="$(jq -cn --argjson names "$1" '{spec: {apiSpec: {policies: [$names[] | {name: .}]}}}')"
  kubectl patch eventgatewaylistenerpolicychains.configuration.konghq.com "${CHAIN_NAME}" -n "${NAMESPACE}" \
    --type=merge -p "${patch}" >/dev/null
}

# A listener takes a single portMapping forward policy, so only first forwards;
# every other policy is a tlsServer policy using the test's server certificate.
create_policy() {
  jq -n --arg name "$1" --arg remote_name "$2" --arg ns "${NAMESPACE}" \
    --arg listener "${LISTENER_NAME}" --arg vc "${VIRTUAL_CLUSTER_NAME}" '
    {apiVersion: "configuration.konghq.com/v1alpha1", kind: "EventGatewayListenerPolicy",
     metadata: {name: $name, namespace: $ns},
     spec: {eventGatewayListenerRef: {type: "namespacedRef", namespacedRef: {name: $listener}},
       apiSpec: (if $name == "first" then
         {type: "forwardToVirtualCluster",
          forwardToVirtualCluster: {name: $remote_name, enabled: "Enabled",
            config: {type: "portMapping", portMapping: {bootstrapPort: "at_start",
              advertisedHost: "placeholder.invalid", destination: {name: $vc}}}}}
       else
         {type: "tlsServer",
          tlsServer: {name: $remote_name, enabled: "Enabled",
            config: {certificates: [{
              certificate: {type: "secretRef", secretRef: {name: "server-tls", key: "tls.crt"}},
              key: {type: "secretRef", secretRef: {name: "server-tls", key: "tls.key"}}}]}}}
       end)}}
  ' | kubectl apply -f - >/dev/null
}

wait_policy() {
  RESOURCE_TYPE=eventgatewaylistenerpolicies.configuration.konghq.com RESOURCE_NAME="$1" \
    JQ_CONDITION='.metadata.generation as $g | .metadata.deletionTimestamp == null and
      ((.status.id // "") != "") and any(.status.conditions[]?;
        .type == "Programmed" and .status == "True" and .observedGeneration == $g)' \
    bash "${COMMON}/wait_for_resource_reconciled.sh" >&2
}

delete_resource() {
  kubectl delete "$1" "$2" -n "${NAMESPACE}" --wait=false >/dev/null
  kubectl wait --for=delete "$1/$2" -n "${NAMESPACE}" --timeout=120s >/dev/null
}

restore_auth_secret() {
  local status="$?"
  trap - EXIT
  jq -n '
    {apiVersion: "v1", kind: "Secret",
     metadata: {name: env.AUTH_SECRET_NAME, namespace: env.NAMESPACE,
                labels: {"konghq.com/secret": "true", "konghq.com/credential": "konnect"}},
     type: "Opaque", stringData: {token: env.KONNECT_TOKEN}}
  ' | kubectl apply -f - >/dev/null || fail "could not restore the test credential Secret"
  RESOURCE_TYPE=konnectapiauthconfigurations.konnect.konghq.com RESOURCE_NAME="${AUTH_NAME}" CONDITION_TYPE=APIAuthValid \
    JQ_CONDITION='.metadata.generation as $g | any(.status.conditions[]?;
      .type == "APIAuthValid" and .status == "True" and .observedGeneration == $g)' \
    bash "${COMMON}/wait_for_resource_reconciled.sh" >&2 || fail "the test authentication did not recover after restoration"
  exit "${status}"
}

snapshot() {
  local names="$1" require_programmed="${2:-true}" name entity policies='{}' listener
  listener="$(RESOURCE_TYPE=eventgatewaylisteners.configuration.konghq.com RESOURCE_NAME="${LISTENER_NAME}" \
    COLLECTION_PATH_JQ='"/v1/event-gateways/" + .status.gatewayID.id + "/listeners"' \
    bash "${COMMON}/konnect_entity_of_resource.sh")"
  while IFS= read -r name; do
    if [[ "${require_programmed}" == true ]]; then
      wait_policy "${name}"
    fi
    entity="$(RESOURCE_TYPE=eventgatewaylistenerpolicies.configuration.konghq.com RESOURCE_NAME="${name}" \
      COLLECTION_PATH_JQ='"/v1/event-gateways/" + .status.gatewayID.id + "/listeners/" + .status.eventGatewayListenerID.id + "/policies"' \
      bash "${COMMON}/konnect_entity_of_resource.sh")"
    [[ "$(jq -er '.collection_path' <<<"${entity}")" == "$(jq -er '.entity_path + "/policies"' <<<"${listener}")" ]] ||
      fail "policy ${name} does not share this listener and gateway"
    policies="$(jq -cn --argjson policies "${policies}" --arg name "${name}" --argjson entity "${entity}" \
      '$policies + {($name): $entity}')"
  done < <(jq -er '.[]' <<<"${names}" || [[ "${names}" == '[]' ]])
  jq -cn --argjson listener "${listener}" --argjson policies "${policies}" --argjson names "${names}" '
    {listener: $listener, policies: $policies,
     chain_path: ($listener.entity_path + "/policy-chain"),
     order: [$names[] | $policies[.].id]}
    | select((.order | length) == (.order | unique | length))
  ' || fail "the complete order must contain distinct nonempty policy IDs"
}

assert_state() {
  local state="$1" retries="$2" reason="${3:-}"
  export EXPECTED_REASON="${reason}"
  if [[ "${state}" == retained ]]; then
    [[ -z "$(kubectl get eventgatewaylistenerpolicychains.configuration.konghq.com "${CHAIN_NAME}" -n "${NAMESPACE}" --ignore-not-found -o name)" ]] ||
      fail "deleted chain CR still exists"
  else
    local condition
    # shellcheck disable=SC2016 # jq reads exported values, not shell interpolation.
    condition='.metadata.generation as $g | ((.status.id // "") == "") and
      any(.status.conditions[]?; .type == "Programmed" and .observedGeneration == $g and .status == "False")'
    case "${state}" in
      ready)
        # shellcheck disable=SC2016
        condition='.metadata.generation as $g | .status.conditions as $conditions | ((.status.id // "") == "") and
          (.spec.apiSpec.policies | map(.name)) == ($ENV.EXPECTED_NAMES | fromjson) and
          .status.eventGatewayListenerID.id == ($ENV.SNAPSHOT | fromjson | .listener.id) and
          all(["EventGatewayListenerRefValid", "APIAuthResolvedRef", "APIAuthValid",
               "KonnectReferencesResolved", "PolicyChainOwnerValid", "Programmed"][];
            . as $type | any($conditions[]?;
              .type == $type and .status == "True" and .observedGeneration == $g))'
        ;;
      reference)
        # shellcheck disable=SC2016
        condition+=' and any(.status.conditions[]?; .type == "KonnectReferencesResolved" and
          .status == "False" and .observedGeneration == $g and .reason == $ENV.EXPECTED_REASON)'
        ;;
      mismatch)
        # shellcheck disable=SC2016
        condition+=' and any(.status.conditions[]?; .type == "KonnectReferencesResolved" and
          .status == "True" and .observedGeneration == $g) and
          any(.status.conditions[]?; .type == "Programmed" and
            (.message | contains("complete remote listener policy chain")))'
        ;;
      conflict)
        # shellcheck disable=SC2016
        condition+=' and any(.status.conditions[]?; .type == "PolicyChainOwnerValid" and
          .status == "False" and .observedGeneration == $g and .reason == "ConflictingChain")'
        ;;
      *)
        fail "unknown observation state ${state}"
        ;;
    esac
    RESOURCE_TYPE=eventgatewaylistenerpolicychains.configuration.konghq.com RESOURCE_NAME="${CHAIN_NAME}" \
      MAX_RETRIES="${retries}" JQ_CONDITION="${condition}" \
      bash "${COMMON}/wait_for_resource_reconciled.sh" >&2
  fi

  local parent claim uid
  parent="$(kubectl get eventgatewaylisteners.configuration.konghq.com "${LISTENER_NAME}" -n "${NAMESPACE}" -o json)"
  jq -e --argjson snapshot "${SNAPSHOT}" '
    .metadata.uid == $snapshot.listener.uid and .status.id == $snapshot.listener.id
    and .metadata.deletionTimestamp == null
  ' <<<"${parent}" >/dev/null || fail "listener identity changed during observation"
  claim="$(jq -r '.metadata.annotations["gateway-operator.konghq.com/listener-policy-chain-owner"] // ""' <<<"${parent}")"
  if [[ "${state}" == retained ]]; then
    [[ -z "${claim}" ]] || fail "chain deletion did not release listener ownership"
  else
    local owner="${CHAIN_NAME}"
    [[ "${state}" != conflict ]] || owner=chain
    uid="$(kubectl get eventgatewaylistenerpolicychains.configuration.konghq.com "${owner}" -n "${NAMESPACE}" -o jsonpath='{.metadata.uid}')"
    [[ "${claim}" == "${owner}/${uid}" ]] || fail "listener ownership does not match the expected chain UID"
  fi
}

observe() {
  local names="$1" state="$2" reason="${3:-}" start name resource id
  export EXPECTED_NAMES="${names}" SNAPSHOT API_PATH EXPECTED_IDS EXPECTED_JQ
  if [[ "${state}" == retained ]]; then
    SNAPSHOT="$(snapshot "${names}" false)"
  else
    SNAPSHOT="$(snapshot "${names}")"
  fi
  API_PATH="$(jq -er '.chain_path' <<<"${SNAPSHOT}")"
  EXPECTED_IDS="$(jq -c '.order' <<<"${SNAPSHOT}")"
  # shellcheck disable=SC2016
  EXPECTED_JQ='type == "object" and has("policies") and (.policies | type == "array") and .policies == ($ENV.EXPECTED_IDS | fromjson)'
  assert_state "${state}" 150 "${reason}"
  RETRY_COUNT=150 bash "${COMMON}/konnect_api_request.sh" >&2
  start="${SECONDS}"
  while true; do
    while IFS= read -r name; do
      resource="$(kubectl get eventgatewaylistenerpolicies.configuration.konghq.com "${name}" -n "${NAMESPACE}" -o json)"
      jq -e --argjson snapshot "${SNAPSHOT}" --arg name "${name}" '
        .metadata.uid == $snapshot.policies[$name].uid and .status.id == $snapshot.policies[$name].id
        and .metadata.deletionTimestamp == null
      ' <<<"${resource}" >/dev/null || fail "policy ${name} changed identity or was deleted during observation"
      id="$(jq -er '.status.id' <<<"${resource}")"
      # shellcheck disable=SC2016
      API_PATH="$(jq -er --arg name "${name}" '.policies[$name].entity_path' <<<"${SNAPSHOT}")" WANT_ID="${id}" \
        EXPECTED_JQ='.id == $ENV.WANT_ID and (.type == "forward_to_virtual_cluster" or .type == "tls_server")' RETRY_COUNT=1 \
        bash "${COMMON}/konnect_api_request.sh" >&2
    done < <(jq -er '.[]' <<<"${names}" || [[ "${names}" == '[]' ]])
    [[ "${HOLD:-false}" == true && $((SECONDS - start)) -lt "${WINDOW_SECONDS}" ]] || break
    sleep "${CHECK_INTERVAL}"
    assert_state "${state}" 1 "${reason}"
    RETRY_COUNT=1 bash "${COMMON}/konnect_api_request.sh" >&2
  done
}

case "${MODE}" in
  bootstrap)
    create_policy first first
    wait_policy first
    create_policy second second
    wait_policy second
    create_chain chain '["second","first"]'
    observe '["second","first"]' ready
    main_snapshot="${SNAPSHOT}"
    LISTENER_NAME=empty-listener
    CHAIN_NAME=empty-chain
    create_chain empty-chain '[]'
    observe '[]' ready
    empty_snapshot="${SNAPSHOT}"
    delete_resource eventgatewaylistenerpolicychains.configuration.konghq.com empty-chain
    observe '[]' retained
    [[ "${empty_snapshot}" == "${SNAPSHOT}" ]] || fail "empty-chain deletion changed its listener"
    SNAPSHOT="${main_snapshot}"
    ;;
  reorder)
    set_order '["first","second"]'
    observe '["first","second"]' ready
    ;;
  drift)
    drift_snapshot="$(snapshot '["second","first"]')"
    export API_PATH EXPECTED_IDS BODY EXPECTED_JQ
    API_PATH="$(jq -er '.chain_path' <<<"${drift_snapshot}")"
    EXPECTED_IDS="$(jq -c '.order' <<<"${drift_snapshot}")"
    # shellcheck disable=SC2016
    BODY='{policies: ($ENV.EXPECTED_IDS | fromjson)}'
    # shellcheck disable=SC2016
    EXPECTED_JQ='.policies == ($ENV.EXPECTED_IDS | fromjson)'
    METHOD=PUT bash "${COMMON}/konnect_api_request.sh" >&2
    unset BODY
    observe '["first","second"]' ready
    ;;
  missing)
    set_order '["first","absent"]'
    observe '["first","second"]' reference ReferenceNotFound
    set_order '["first","second"]'
    observe '["first","second"]' ready
    ;;
  unready)
    # A duplicate Konnect name belongs to second, not this CR. It cannot acquire
    # that entity's ID, giving a deterministic unprogrammed same-listener policy.
    create_policy pending second
    RESOURCE_TYPE=eventgatewaylistenerpolicies.configuration.konghq.com RESOURCE_NAME=pending \
      JQ_CONDITION='((.status.id // "") == "") and any(.status.conditions[]?;
        .type == "Programmed" and .status == "False")' \
      bash "${COMMON}/wait_for_resource_reconciled.sh" >&2
    set_order '["pending","first","second"]'
    observe '["first","second"]' reference ReferenceNotProgrammed
    kubectl patch eventgatewaylistenerpolicies.configuration.konghq.com pending -n "${NAMESPACE}" \
      --type=merge -p '{"spec":{"apiSpec":{"tlsServer":{"name":"pending"}}}}' >/dev/null
    wait_policy pending
    observe '["pending","first","second"]' ready
    ;;
  recreate)
    ORIGINAL="$(snapshot '["pending","first","second"]')"
    delete_resource eventgatewaylistenerpolicies.configuration.konghq.com first
    API_PATH="$(jq -er '.policies.first.entity_path' <<<"${ORIGINAL}")" \
      EXPECTED_STATUS=404 EXPECTED_JQ='' RETRY_COUNT=30 bash "${COMMON}/konnect_api_request.sh" >&2
    observe '["pending","second"]' reference ReferenceNotFound
    create_policy first first
    wait_policy first
    observe '["pending","first","second"]' ready
    jq -en --argjson original "${ORIGINAL}" --argjson current "${SNAPSHOT}" '
      $original.listener == $current.listener and $original.chain_path == $current.chain_path and
      $original.policies.first.id != $current.policies.first.id and
      $original.policies.first.uid != $current.policies.first.uid and
      $original.policies.second == $current.policies.second and
      $original.policies.pending == $current.policies.pending
    ' >/dev/null || fail "recreation did not replace only first with its latest ID and UID"
    ;;
  policy-set)
    create_policy third third
    wait_policy third
    observe '["pending","first","second","third"]' mismatch
    set_order '["third","second","first","pending"]'
    observe '["third","second","first","pending"]' ready
    third_path="$(jq -er '.policies.third.entity_path' <<<"${SNAPSHOT}")"
    set_order '["second","first","pending"]'
    observe '["third","second","first","pending"]' mismatch
    delete_resource eventgatewaylistenerpolicies.configuration.konghq.com third
    API_PATH="${third_path}" EXPECTED_STATUS=404 EXPECTED_JQ='' RETRY_COUNT=30 bash "${COMMON}/konnect_api_request.sh" >&2
    observe '["second","first","pending"]' ready
    ;;
  handover)
    create_chain contender '["pending","first","second"]'
    CHAIN_NAME=contender
    observe '["second","first","pending"]' conflict
    delete_resource eventgatewaylistenerpolicychains.configuration.konghq.com chain
    observe '["pending","first","second"]' ready
    ;;
  retained)
    CHAIN_NAME=contender
    ORIGINAL="$(snapshot '["pending","first","second"]')"
    set_order '["absent"]'
    observe '["pending","first","second"]' reference ReferenceNotFound
    trap restore_auth_secret EXIT
    trap 'exit 1' HUP INT TERM
    # Referenced auth resources have in-use finalizers; remove only our Secret's token key.
    kubectl patch secret "${AUTH_SECRET_NAME}" -n "${NAMESPACE}" \
      --type=merge -p '{"data":{"token":null}}' >/dev/null
    RESOURCE_TYPE=konnectapiauthconfigurations.konnect.konghq.com RESOURCE_NAME="${AUTH_NAME}" CONDITION_TYPE=APIAuthValid \
      JQ_CONDITION='.metadata.generation as $g | any(.status.conditions[]?;
        .type == "APIAuthValid" and .status == "False" and .reason == "Invalid" and .observedGeneration == $g)' \
      bash "${COMMON}/wait_for_resource_reconciled.sh" >&2
    delete_resource eventgatewaylistenerpolicychains.configuration.konghq.com contender
    HOLD=true observe '["pending","first","second"]' retained
    jq -en --argjson original "${ORIGINAL}" --argjson current "${SNAPSHOT}" \
      '$original == $current' >/dev/null || fail "chain deletion changed remote order, policy IDs/UIDs, or listener identity"
    ;;
  *)
    fail "unknown lifecycle scenario ${MODE}"
    ;;
esac
jq -cn --argjson snapshot "${SNAPSHOT}" '$snapshot'
