#!/usr/bin/env bash
# Creates an Event Gateway in Konnect via the REST API and outputs its ID.
# Retries on transient failures. Outputs a JSON object for Chainsaw to parse
# as a script output binding.
#
# Variables (from environment):
#   KONNECT_TOKEN:      Konnect PAT / system account token.
#   KONNECT_SERVER_URL: Konnect API server hostname (e.g. "eu.api.konghq.tech").
#   GATEWAY_NAME:       Human-readable name for the new Event Gateway.
#   MAX_RETRIES:        (optional) Maximum number of retry attempts. Default: 180.
#   RETRY_DELAY:        (optional) Delay in seconds between retries. Default: 1.
#   GATEWAY_ID_FILE:    (optional) Path to write the created gateway ID for cross-step use.
#                       Default: /tmp/konnect_event_gateway_id.

set -o errexit
set -o nounset
set -o pipefail

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_SERVER_URL="${KONNECT_SERVER_URL}"
GATEWAY_NAME="${GATEWAY_NAME}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
GATEWAY_ID_FILE="${GATEWAY_ID_FILE:-/tmp/konnect_event_gateway_id}"

API_URL="https://${KONNECT_SERVER_URL}/v1/event-gateways"
# Redact token from any logged curl command.
CURL_CMD="curl --silent --show-error --request POST ${API_URL} --header 'Authorization: Bearer [REDACTED]' --header 'Content-Type: application/json' --data '{\"name\": \"${GATEWAY_NAME}\"}'"

RESPONSE_BODY_FILE="/tmp/konnect_create_response.json"
CURL_STDERR_FILE="/tmp/konnect_create_stderr.txt"

GATEWAY_ID=""
LAST_HTTP_STATUS=""
LAST_RESPONSE=""
LAST_CURL_STDERR=""

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  LAST_HTTP_STATUS=$(curl --silent --show-error \
    --request POST "${API_URL}" \
    --header "Authorization: Bearer ${KONNECT_TOKEN}" \
    --header "Content-Type: application/json" \
    --data "$(jq -n --arg name "${GATEWAY_NAME}" '{"name": $name}')" \
    --output "${RESPONSE_BODY_FILE}" \
    --write-out '%{http_code}' \
    2>"${CURL_STDERR_FILE}") || true

  LAST_RESPONSE=$(cat "${RESPONSE_BODY_FILE}" 2>/dev/null || echo '{}')
  LAST_CURL_STDERR=$(cat "${CURL_STDERR_FILE}" 2>/dev/null || echo '')

  if [ "${LAST_HTTP_STATUS}" = "201" ]; then
    GATEWAY_ID=$(echo "${LAST_RESPONSE}" | jq -r '.id // empty')
    if [ -n "${GATEWAY_ID}" ]; then
      break
    fi
  fi

  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then
    sleep "${RETRY_DELAY}"
  fi
done

if [ -z "${GATEWAY_ID}" ]; then
  cat <<EOF
{
  "error": "Failed to create Event Gateway in Konnect after ${MAX_RETRIES} attempts",
  "api_url": "${API_URL}",
  "gateway_name": "${GATEWAY_NAME}",
  "curl_command": $(echo "${CURL_CMD}" | jq -Rs .),
  "last_http_status": "${LAST_HTTP_STATUS}",
  "last_response": $(echo "${LAST_RESPONSE}" | jq -Rs .),
  "curl_stderr": $(echo "${LAST_CURL_STDERR}" | jq -Rs .),
  "retry_attempt": ${MAX_RETRIES},
  "max_retries": ${MAX_RETRIES}
}
EOF
  exit 1
fi

# Persist the ID to a file so subsequent steps and catch blocks can read it
# even though Chainsaw does not propagate script outputs across steps.
echo -n "${GATEWAY_ID}" > "${GATEWAY_ID_FILE}"

cat <<EOF
{
  "id": "${GATEWAY_ID}",
  "gateway_name": "${GATEWAY_NAME}",
  "api_url": "${API_URL}",
  "gateway_id_file": "${GATEWAY_ID_FILE}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
