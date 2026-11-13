#!/bin/bash
# Send a request to the Konnect API and, optionally, retry until the response
# matches the expected HTTP status(es) and a jq condition. Used both to verify
# what the operator did on the Konnect side and to set up (or clean up) state
# behind its back.
#
# Prints {"status": <http status>, "body": <response JSON or null>,
# "retry_attempt": <n>} on success, so chainsaw outputs can read the response,
# e.g. (json_parse($stdout).body.id).
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   KONNECT_TOKEN: Bearer token for the Konnect API.
#   KONNECT_URL: Konnect server host, e.g. eu.api.konghq.tech.
#   API_PATH: Request path, e.g. '/v1/ai-gateways/<id>/custom-policies/<name>'.
#   METHOD: (optional) HTTP method. Default: GET.
#   BODY: (optional) Request body, as a jq program evaluated with 'jq -n', so
#     it can be a JSON literal or reference environment variables via $ENV,
#     e.g. '{name: $ENV.POLICY_NAME, type: "streaming"}'.
#   EXPECTED_STATUS: (optional) Comma-separated list of accepted HTTP statuses.
#     Default: any 2xx.
#   EXPECTED_JQ: (optional) jq condition the response body must satisfy. It can
#     reference environment variables via $ENV, e.g.
#     '.id == $ENV.WANT_ID and .labels["k8s-uid"] == $ENV.WANT_UID'.
#   RETRY_COUNT: (optional) Number of attempts. Default: 120 for GET, 1 for
#     other methods (a mutating request is not repeated by default).
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.
#   CONNECT_TIMEOUT: (optional) curl connect timeout in seconds. Default: 3.
#   MAX_TIME: (optional) curl per-try timeout in seconds. Default: 5.
#   CURL_RETRIES: (optional) Times curl resends a request within one attempt
#     on a transport error or a 408, 429, 500, 502, 503 or 504. Default: 3,
#     0 for POST, which may have been applied before the response was lost.

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_URL="${KONNECT_URL}"
API_PATH="${API_PATH}"
METHOD="${METHOD:-GET}"
BODY="${BODY:-}"
EXPECTED_STATUS="${EXPECTED_STATUS:-}"
EXPECTED_JQ="${EXPECTED_JQ:-}"
if [[ "${METHOD}" == "GET" ]]; then
  RETRY_COUNT="${RETRY_COUNT:-120}"
else
  RETRY_COUNT="${RETRY_COUNT:-1}"
fi
RETRY_DELAY="${RETRY_DELAY:-1}"
CONNECT_TIMEOUT="${CONNECT_TIMEOUT:-3}"
MAX_TIME="${MAX_TIME:-5}"
if [[ "${METHOD}" == "POST" ]]; then
  CURL_RETRIES="${CURL_RETRIES:-0}"
else
  CURL_RETRIES="${CURL_RETRIES:-3}"
fi

RESPONSE_FILE="$(mktemp)"
trap 'rm -f "${RESPONSE_FILE}"' EXIT

fail() {
  jq -n \
    --arg error "$1" \
    --arg method "${METHOD}" \
    --arg path "${API_PATH}" \
    --arg response "$(cat "${RESPONSE_FILE}" 2>/dev/null || true)" \
    '{error: $error, method: $method, path: $path, last_response: $response}'
  exit 1
}

# Without --fail, --retry-all-errors resends on transport errors and on 408,
# 429, 500, 502, 503 and 504 only, whatever EXPECTED_STATUS accepts.
# EXPECTED_STATUS and EXPECTED_JQ only judge the final response.
CURL_ARGS=(-s -o "${RESPONSE_FILE}" -w '%{http_code}' -X "${METHOD}"
  --connect-timeout "${CONNECT_TIMEOUT}" --max-time "${MAX_TIME}"
  --retry "${CURL_RETRIES}" --retry-all-errors --retry-delay 1
  -H @-)
if [[ -n "${BODY}" ]]; then
  REQUEST_BODY="$(jq -cn "${BODY}")" || fail "BODY is not a valid jq program"
  CURL_ARGS+=(-H "Content-Type: application/json" -d "${REQUEST_BODY}")
fi

status_accepted() {
  local status="$1"
  if [[ -z "${EXPECTED_STATUS}" ]]; then
    [[ "${status}" == 2?? ]]
    return
  fi
  [[ ",${EXPECTED_STATUS}," == *",${status},"* ]]
}

LAST_ERROR=""
for ATTEMPT in $(seq 1 "${RETRY_COUNT}"); do
  # Keep the credential in a pipe, not curl's argv or a temporary header file.
  STATUS="$(printf 'Authorization: Bearer %s\n' "${KONNECT_TOKEN}" |
    curl "${CURL_ARGS[@]}" "https://${KONNECT_URL}${API_PATH}" || true)"
  if ! status_accepted "${STATUS}"; then
    LAST_ERROR="HTTP status ${STATUS}, expected ${EXPECTED_STATUS:-2xx}"
  elif [[ -n "${EXPECTED_JQ}" ]] && ! jq -e "${EXPECTED_JQ}" "${RESPONSE_FILE}" >/dev/null 2>&1; then
    LAST_ERROR="response does not satisfy: ${EXPECTED_JQ}"
  else
    RESPONSE_BODY="$(jq -c . "${RESPONSE_FILE}" 2>/dev/null || echo null)"
    jq -n \
      --argjson status "${STATUS}" \
      --argjson body "${RESPONSE_BODY:-null}" \
      --argjson attempt "${ATTEMPT}" \
      '{status: $status, body: $body, retry_attempt: $attempt}'
    exit 0
  fi
  if [[ "${ATTEMPT}" -lt "${RETRY_COUNT}" ]]; then
    sleep "${RETRY_DELAY}"
  fi
done
fail "${LAST_ERROR}"
