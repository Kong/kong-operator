#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Talks to the Konnect AI Gateway custom policies API directly, to verify what
# the operator did on the Konnect side (or to set up state behind its back).
#
# The Konnect AI Gateway ID is read from the KonnectAIGateway's status.id.
# Custom policies are addressed by name: the API accepts an ID or a name in
# /v1/ai-gateways/{gatewayId}/custom-policies/{customPolicyIdOrName}, and the
# name is unique and immutable.
#
# Variables (from environment):
#   ACTION: One of:
#     create         - POST a custom policy directly to Konnect (bypassing the
#                      operator). Requires POLICY_TYPE, POLICY_DISPLAY_NAME.
#                      Prints {"id": "<konnect id>"}.
#     assert-present - Wait until the custom policy exists in Konnect and
#                      matches every EXPECTED_* variable that is set.
#     assert-absent  - Wait until Konnect returns 404 for the custom policy.
#   NAMESPACE: Namespace of the KonnectAIGateway (and AIGatewayCustomPolicy).
#   AIGW_CP_NAME: Name of the KonnectAIGateway.
#   POLICY_NAME: The custom policy name (spec.apiSpec.<type>.name).
#   KONNECT_TOKEN: Bearer token for the Konnect API.
#   KONNECT_URL: Konnect server host, e.g. eu.api.konghq.tech.
#   POLICY_TYPE: (create) "installed" or "streaming".
#   POLICY_DISPLAY_NAME: (create) Display name.
#   EXPECTED_TYPE: (assert-present, optional) Expected "type".
#   EXPECTED_DISPLAY_NAME: (assert-present, optional) Expected "display_name".
#   EXPECTED_ID: (assert-present, optional) Expected Konnect ID.
#   EXPECTED_CR_NAME: (assert-present, optional) Name of an AIGatewayCustomPolicy
#     whose status.id must equal the Konnect ID.
#   EXPECTED_HANDLER_CONTAINS: (assert-present, optional) Substring the
#     "handler" must contain.
#   EXPECT_UPDATED: (assert-present, optional) "true" to require updated_at to
#     differ from created_at, i.e. the policy was updated in place rather than
#     recreated.
#   RETRY_COUNT: (Optional) Number of retries. Default: 120.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

ACTION="${ACTION}"
NAMESPACE="${NAMESPACE}"
AIGW_CP_NAME="${AIGW_CP_NAME}"
POLICY_NAME="${POLICY_NAME}"
KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
EXPECTED_TYPE="${EXPECTED_TYPE:-}"
EXPECTED_DISPLAY_NAME="${EXPECTED_DISPLAY_NAME:-}"
EXPECTED_ID="${EXPECTED_ID:-}"
EXPECTED_CR_NAME="${EXPECTED_CR_NAME:-}"
EXPECTED_HANDLER_CONTAINS="${EXPECTED_HANDLER_CONTAINS:-}"
EXPECT_UPDATED="${EXPECT_UPDATED:-false}"
RETRY_COUNT="${RETRY_COUNT:-120}"
RETRY_DELAY="${RETRY_DELAY:-1}"

RESPONSE_FILE="$(mktemp)"
trap 'rm -f "${RESPONSE_FILE}"' EXIT

fail() {
  jq -n \
    --arg error "$1" \
    --arg action "${ACTION}" \
    --arg policy_name "${POLICY_NAME}" \
    --arg response "$(cat "${RESPONSE_FILE}" 2>/dev/null || true)" \
    '{error: $error, action: $action, policy_name: $policy_name, last_response: $response}'
  exit 1
}

GATEWAY_ID=""
for _ in $(seq 1 "${RETRY_COUNT}"); do
  GATEWAY_ID="$(kubectl get konnectaigateways.konnect.konghq.com "${AIGW_CP_NAME}" \
    -n "${NAMESPACE}" -o jsonpath='{.status.id}' 2>/dev/null || true)"
  [[ -n "${GATEWAY_ID}" ]] && break
  sleep "${RETRY_DELAY}"
done
[[ -n "${GATEWAY_ID}" ]] || fail "KonnectAIGateway ${NAMESPACE}/${AIGW_CP_NAME} has no status.id"

BASE_URL="https://${KONNECT_URL}/v1/ai-gateways/${GATEWAY_ID}/custom-policies"

get_policy() {
  curl -s -o "${RESPONSE_FILE}" -w '%{http_code}' \
    -H "Authorization: Bearer ${KONNECT_TOKEN}" \
    "${BASE_URL}/${POLICY_NAME}" || true
}

case "${ACTION}" in
  create)
    POLICY_TYPE="${POLICY_TYPE}"
    POLICY_DISPLAY_NAME="${POLICY_DISPLAY_NAME}"
    SCHEMA="return { name = \"${POLICY_NAME}\", fields = { { config = { type = \"record\", fields = {} } } } }"
    HANDLER="return { PRIORITY = 1000, VERSION = \"1.0.0\" }"
    BODY="$(jq -n \
      --arg name "${POLICY_NAME}" \
      --arg type "${POLICY_TYPE}" \
      --arg display_name "${POLICY_DISPLAY_NAME}" \
      --arg schema "${SCHEMA}" \
      --arg handler "${HANDLER}" \
      '{name: $name, type: $type, display_name: $display_name, schema: $schema}
       + (if $type == "streaming" then {handler: $handler} else {} end)')"

    STATUS="$(curl -s -o "${RESPONSE_FILE}" -w '%{http_code}' \
      -X POST \
      -H "Authorization: Bearer ${KONNECT_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "${BODY}" \
      "${BASE_URL}" || true)"
    [[ "${STATUS}" == "201" || "${STATUS}" == "200" ]] || fail "creating custom policy returned HTTP ${STATUS}"

    ID="$(jq -r '.id // empty' "${RESPONSE_FILE}")"
    [[ -n "${ID}" ]] || fail "create response has no id"
    jq -n --arg id "${ID}" '{id: $id}'
    ;;

  assert-present)
    LAST_ERROR=""
    for ATTEMPT in $(seq 1 "${RETRY_COUNT}"); do
      STATUS="$(get_policy)"
      if [[ "${STATUS}" != "200" ]]; then
        LAST_ERROR="Konnect returned HTTP ${STATUS}"
        sleep "${RETRY_DELAY}"
        continue
      fi

      ID="$(jq -r '.id // empty' "${RESPONSE_FILE}")"
      TYPE="$(jq -r '.type // empty' "${RESPONSE_FILE}")"
      DISPLAY_NAME="$(jq -r '.display_name // empty' "${RESPONSE_FILE}")"
      HANDLER="$(jq -r '.handler // empty' "${RESPONSE_FILE}")"
      CREATED_AT="$(jq -r '.created_at // empty' "${RESPONSE_FILE}")"
      UPDATED_AT="$(jq -r '.updated_at // empty' "${RESPONSE_FILE}")"

      WANT_ID="${EXPECTED_ID}"
      if [[ -n "${EXPECTED_CR_NAME}" ]]; then
        CR_ID="$(kubectl get aigatewaycustompolicies.aiconfiguration.konghq.com "${EXPECTED_CR_NAME}" \
          -n "${NAMESPACE}" -o jsonpath='{.status.id}' 2>/dev/null || true)"
        if [[ -z "${CR_ID}" ]]; then
          LAST_ERROR="AIGatewayCustomPolicy ${EXPECTED_CR_NAME} has no status.id yet"
          sleep "${RETRY_DELAY}"
          continue
        fi
        if [[ -n "${WANT_ID}" && "${WANT_ID}" != "${CR_ID}" ]]; then
          LAST_ERROR="AIGatewayCustomPolicy ${EXPECTED_CR_NAME} status.id is ${CR_ID}, expected ${WANT_ID}"
          sleep "${RETRY_DELAY}"
          continue
        fi
        WANT_ID="${CR_ID}"
      fi

      if [[ -n "${WANT_ID}" && "${ID}" != "${WANT_ID}" ]]; then
        LAST_ERROR="Konnect id is ${ID}, expected ${WANT_ID}"
      elif [[ -n "${EXPECTED_TYPE}" && "${TYPE}" != "${EXPECTED_TYPE}" ]]; then
        LAST_ERROR="Konnect type is ${TYPE}, expected ${EXPECTED_TYPE}"
      elif [[ -n "${EXPECTED_DISPLAY_NAME}" && "${DISPLAY_NAME}" != "${EXPECTED_DISPLAY_NAME}" ]]; then
        LAST_ERROR="Konnect display_name is '${DISPLAY_NAME}', expected '${EXPECTED_DISPLAY_NAME}'"
      elif [[ -n "${EXPECTED_HANDLER_CONTAINS}" && "${HANDLER}" != *"${EXPECTED_HANDLER_CONTAINS}"* ]]; then
        LAST_ERROR="Konnect handler does not contain '${EXPECTED_HANDLER_CONTAINS}'"
      elif [[ "${EXPECT_UPDATED}" == "true" && "${UPDATED_AT}" == "${CREATED_AT}" ]]; then
        LAST_ERROR="Konnect updated_at equals created_at (${CREATED_AT}); expected an in-place update"
      else
        jq -n \
          --arg id "${ID}" \
          --arg type "${TYPE}" \
          --arg display_name "${DISPLAY_NAME}" \
          --arg created_at "${CREATED_AT}" \
          --arg updated_at "${UPDATED_AT}" \
          --argjson attempt "${ATTEMPT}" \
          '{success: true, id: $id, type: $type, display_name: $display_name,
            created_at: $created_at, updated_at: $updated_at, retry_attempt: $attempt}'
        exit 0
      fi
      sleep "${RETRY_DELAY}"
    done
    fail "${LAST_ERROR}"
    ;;

  assert-absent)
    for ATTEMPT in $(seq 1 "${RETRY_COUNT}"); do
      STATUS="$(get_policy)"
      if [[ "${STATUS}" == "404" ]]; then
        jq -n --argjson attempt "${ATTEMPT}" '{success: true, retry_attempt: $attempt}'
        exit 0
      fi
      sleep "${RETRY_DELAY}"
    done
    fail "custom policy still present in Konnect (last HTTP status ${STATUS})"
    ;;

  *)
    fail "unknown ACTION '${ACTION}'"
    ;;
esac
