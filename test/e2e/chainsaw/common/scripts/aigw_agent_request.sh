#!/usr/bin/env bash
# Drive an HTTP request through an AIGatewayDataPlane to an AIGatewayAgent.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
DP_SVC="${DP_SVC}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH}"
EXPECTED_BODY="${EXPECTED_BODY:-}"
UNEXPECTED_BODY="${UNEXPECTED_BODY:-}"
EXPECTED_JSONRPC="${EXPECTED_JSONRPC:-}"
EXPECTED_JSON_ID="${EXPECTED_JSON_ID:-}"
EXPECTED_JSON_RESULT_TEXT="${EXPECTED_JSON_RESULT_TEXT:-}"
REQUEST_BODY="${REQUEST_BODY:-}"
CONTENT_TYPE="${CONTENT_TYPE:-application/json}"
EXPECTED_STATUS="${EXPECTED_STATUS:-200}"
EXPECTED_STATUS_NOT="${EXPECTED_STATUS_NOT:-}"
PORT="${PORT:-443}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-5}"

POD="aigw-agent-traffic-${RANDOM}"
URL="https://${DP_SVC}.${NAMESPACE}.svc:${PORT}${ROUTE_PATH}"

cleanup() {
  kubectl -n "${NAMESPACE}" delete pod "${POD}" --ignore-not-found --grace-period=0 --force >/dev/null 2>&1 || true
}
trap cleanup EXIT

kubectl -n "${NAMESPACE}" run "${POD}" \
  --image="${CURL_IMAGE}" --image-pull-policy=IfNotPresent --restart=Never \
  --command -- sleep 3600 >/dev/null
kubectl -n "${NAMESPACE}" wait --for=condition=Ready "pod/${POD}" --timeout=120s >/dev/null

response_matches() {
  local code="$1"
  local body="$2"

  if [ -n "${EXPECTED_STATUS_NOT}" ]; then
    [ -n "${code}" ] && [ "${code}" != "000" ] || return 1
    [ "${code}" != "${EXPECTED_STATUS_NOT}" ] || return 1
  else
    [ "${code}" = "${EXPECTED_STATUS}" ] || return 1
  fi

  if [ -n "${EXPECTED_BODY}" ]; then
    printf '%s' "${body}" | grep -q "${EXPECTED_BODY}" || return 1
  fi

  if [ -n "${UNEXPECTED_BODY}" ]; then
    ! printf '%s' "${body}" | grep -q "${UNEXPECTED_BODY}" || return 1
  fi

  if [ -n "${EXPECTED_JSONRPC}${EXPECTED_JSON_ID}${EXPECTED_JSON_RESULT_TEXT}" ]; then
    command -v jq >/dev/null 2>&1 || {
      echo "jq is required for JSON response assertions" >&2
      return 1
    }
    printf '%s' "${body}" | jq -e . >/dev/null || return 1
  fi

  if [ -n "${EXPECTED_JSONRPC}" ]; then
    [ "$(printf '%s' "${body}" | jq -r '.jsonrpc')" = "${EXPECTED_JSONRPC}" ] || return 1
  fi

  if [ -n "${EXPECTED_JSON_ID}" ]; then
    [ "$(printf '%s' "${body}" | jq -r '.id')" = "${EXPECTED_JSON_ID}" ] || return 1
  fi

  if [ -n "${EXPECTED_JSON_RESULT_TEXT}" ]; then
    [ "$(printf '%s' "${body}" | jq -r '.result.parts[0].text')" = "${EXPECTED_JSON_RESULT_TEXT}" ] || return 1
  fi

  return 0
}

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  OUT="$(kubectl -n "${NAMESPACE}" exec "${POD}" -- sh -c \
    'body="$1"
     method="$2"
     url="$3"
     content_type="$4"
     if [ -n "$body" ]; then
       curl -sk -m 30 -o /tmp/body -w "%{http_code}" -X "$method" -H "Content-Type: $content_type" --data "$body" "$url"
     else
       curl -sk -m 30 -o /tmp/body -w "%{http_code}" -X "$method" "$url"
     fi
     echo
     cat /tmp/body' \
    sh "${REQUEST_BODY}" "${METHOD}" "${URL}" "${CONTENT_TYPE}" \
    2>/dev/null || true)"
  CODE="$(printf '%s\n' "${OUT}" | sed -n '1p')"
  BODY="$(printf '%s\n' "${OUT}" | sed '1d')"
  if response_matches "${CODE}" "${BODY}"; then
    echo "ok: status=${CODE}"
    exit 0
  fi
  echo "attempt ${ATTEMPT}/${MAX_RETRIES}: status='${CODE}' body='${BODY}'" >&2
  sleep "${RETRY_DELAY}"
done

echo "FAILED: response did not match expectations after ${MAX_RETRIES} attempts (last status='${CODE}')" >&2
exit 1
