#!/usr/bin/env bash
# Asserts that an Event Gateway still exists in Konnect via the REST API.
# Retries to account for eventual consistency. Fails if the gateway is not found.
#
# Variables (from environment):
#   KONNECT_TOKEN:      Konnect PAT / system account token.
#   KONNECT_SERVER_URL: Konnect API server hostname (e.g. "eu.api.konghq.tech").
#   GATEWAY_ID_FILE:    Path to the file containing the Event Gateway UUID.
#   MAX_RETRIES:        (optional) Maximum number of retry attempts. Default: 180.
#   RETRY_DELAY:        (optional) Delay in seconds between retries. Default: 1.

set -o errexit
set -o nounset
set -o pipefail

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_SERVER_URL="${KONNECT_SERVER_URL}"
GATEWAY_ID_FILE="${GATEWAY_ID_FILE}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

GATEWAY_ID=$(cat "${GATEWAY_ID_FILE}")
API_URL="https://${KONNECT_SERVER_URL}/v1/event-gateways/${GATEWAY_ID}"
# Redact token from any logged curl command.
CURL_CMD="curl --silent --show-error --request GET ${API_URL} --header 'Authorization: Bearer [REDACTED]'"

RESPONSE_BODY_FILE="/tmp/konnect_assert_response.json"
CURL_STDERR_FILE="/tmp/konnect_assert_stderr.txt"

LAST_HTTP_STATUS=""
LAST_RESPONSE=""
LAST_CURL_STDERR=""

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  LAST_HTTP_STATUS=$(curl --silent --show-error \
    --request GET "${API_URL}" \
    --header "Authorization: Bearer ${KONNECT_TOKEN}" \
    --output "${RESPONSE_BODY_FILE}" \
    --write-out '%{http_code}' \
    2>"${CURL_STDERR_FILE}") || true

  LAST_RESPONSE=$(cat "${RESPONSE_BODY_FILE}" 2>/dev/null || echo '{}')
  LAST_CURL_STDERR=$(cat "${CURL_STDERR_FILE}" 2>/dev/null || echo '')

  if [ "${LAST_HTTP_STATUS}" = "200" ]; then
    cat <<EOF
{
  "gateway_id": "${GATEWAY_ID}",
  "exists_in_konnect": true,
  "api_url": "${API_URL}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
    exit 0
  fi

  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then
    sleep "${RETRY_DELAY}"
  fi
done

cat <<EOF
{
  "error": "Event Gateway not found in Konnect after ${MAX_RETRIES} attempts — Mirror delete must not have removed it",
  "gateway_id": "${GATEWAY_ID}",
  "exists_in_konnect": false,
  "api_url": "${API_URL}",
  "curl_command": $(echo "${CURL_CMD}" | jq -Rs .),
  "last_http_status": "${LAST_HTTP_STATUS}",
  "last_response": $(echo "${LAST_RESPONSE}" | jq -Rs .),
  "curl_stderr": $(echo "${LAST_CURL_STDERR}" | jq -Rs .),
  "retry_attempt": ${MAX_RETRIES},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
