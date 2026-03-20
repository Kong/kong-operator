#!/usr/bin/env bash
# Deletes an Event Gateway in Konnect via the REST API by its ID.
# Retries on transient failures. Used for cleanup after a Mirror-source test
# (the operator does not delete mirrored resources).
#
# Variables (from environment):
#   KONNECT_TOKEN:      Konnect PAT / system account token.
#   KONNECT_SERVER_URL: Konnect API server hostname (e.g. "eu.api.konghq.tech").
#   GATEWAY_ID:         UUID of the Event Gateway to delete.
#                       If not set, GATEWAY_ID_FILE is used to read it.
#   GATEWAY_ID_FILE:    (optional) Path to a file containing the Event Gateway UUID.
#                       Used when GATEWAY_ID is not set directly. If the file does
#                       not exist, the script exits 0 with a skipped message.
#   MAX_RETRIES:        (optional) Maximum number of retry attempts. Default: 180.
#   RETRY_DELAY:        (optional) Delay in seconds between retries. Default: 1.

set -o errexit
set -o nounset
set -o pipefail

KONNECT_TOKEN="${KONNECT_TOKEN}"
KONNECT_SERVER_URL="${KONNECT_SERVER_URL}"
GATEWAY_ID="${GATEWAY_ID:-}"
GATEWAY_ID_FILE="${GATEWAY_ID_FILE:-}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Resolve GATEWAY_ID from file if not provided directly.
if [ -z "${GATEWAY_ID}" ] && [ -n "${GATEWAY_ID_FILE}" ]; then
  GATEWAY_ID=$(cat "${GATEWAY_ID_FILE}" 2>/dev/null || echo '')
fi

if [ -z "${GATEWAY_ID}" ]; then
  echo '{"skipped": true, "reason": "no gateway ID provided"}'
  exit 0
fi

API_URL="https://${KONNECT_SERVER_URL}/v1/event-gateways/${GATEWAY_ID}"
# Redact token from any logged curl command.
CURL_CMD="curl --silent --show-error --request DELETE ${API_URL} --header 'Authorization: Bearer [REDACTED]'"

CURL_STDERR_FILE="/tmp/konnect_delete_stderr.txt"

LAST_HTTP_STATUS=""
LAST_CURL_STDERR=""

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  LAST_HTTP_STATUS=$(curl --silent --show-error \
    --request DELETE "${API_URL}" \
    --header "Authorization: Bearer ${KONNECT_TOKEN}" \
    --output /dev/null \
    --write-out '%{http_code}' \
    2>"${CURL_STDERR_FILE}") || true

  LAST_CURL_STDERR=$(cat "${CURL_STDERR_FILE}" 2>/dev/null || echo '')

  # 204 = deleted, 404 = already gone; both are success.
  if [ "${LAST_HTTP_STATUS}" = "204" ] || [ "${LAST_HTTP_STATUS}" = "404" ]; then
    break
  fi

  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then
    sleep "${RETRY_DELAY}"
  fi
done

if [ "${LAST_HTTP_STATUS}" != "204" ] && [ "${LAST_HTTP_STATUS}" != "404" ]; then
  cat <<EOF
{
  "error": "Failed to delete Event Gateway in Konnect after ${MAX_RETRIES} attempts",
  "api_url": "${API_URL}",
  "gateway_id": "${GATEWAY_ID}",
  "curl_command": $(echo "${CURL_CMD}" | jq -Rs .),
  "last_http_status": "${LAST_HTTP_STATUS}",
  "curl_stderr": $(echo "${LAST_CURL_STDERR}" | jq -Rs .),
  "retry_attempt": ${MAX_RETRIES},
  "max_retries": ${MAX_RETRIES}
}
EOF
  exit 1
fi

cat <<EOF
{
  "gateway_id": "${GATEWAY_ID}",
  "http_status": "${LAST_HTTP_STATUS}",
  "api_url": "${API_URL}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
