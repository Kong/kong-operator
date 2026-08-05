#!/usr/bin/env bash
# Drive an HTTP request through an AIGatewayDataPlane to an AIGatewayAgent.
#
# Assumes ADDRESS is directly reachable from wherever this script runs: either
# the chainsaw test-runner host (e.g. an external LoadBalancer address), or an
# in-cluster curl Pod when piped through aigw_agent_request_from_cluster.sh
# (e.g. the ingress Service's cluster-DNS name). Emits a JSON result object to
# stdout (success or failure) and exits non-zero (failing the chainsaw step)
# unless a matching response is observed within the retry budget.
#
# Required env:
#   ADDRESS     Host or IP to connect to (cluster-DNS name or external address).
#   METHOD      HTTP method to use (e.g. GET, POST).
#   ROUTE_PATH  Route path to request.
#
# Optional env:
#   REQUEST_HEADERS  Extra request headers, one "Name:value" pair per line.
#                     Each line is passed to curl as a separate -H flag.
#                     Default: none.
#   REQUEST_BODY      Request body to send. Default: none.
#   CONTENT_TYPE      Content-Type header used when REQUEST_BODY is set.
#                     Default: application/json.
#   EXPECTED_STATUS   Expected HTTP status code. Default: 200.
#   EXPECTED_STATUS_NOT  If set, assert the status is anything other than this
#                     value (and not a connection failure), instead of matching
#                     EXPECTED_STATUS.
#   EXPECTED_BODY     Substring that must be present in the response body.
#   UNEXPECTED_BODY   Substring that must NOT be present in the response body.
#   EXPECTED_JSONRPC  Expected value of the response body's ".jsonrpc" field.
#   EXPECTED_JSON_ID  Expected value of the response body's ".id" field.
#   EXPECTED_JSON_RESULT_TEXT  Expected value of ".result.parts[0].text".
#   PORT              Ingress Service port. Default: 443.
#   MAX_RETRIES       Retry attempts. Default: 180.
#   RETRY_DELAY       Seconds between retries. Default: 1.
#   OIDC_TOKEN_URL    If set, fetches an access token via the OAuth2 client_credentials
#                     grant from this token endpoint URL (must be reachable from
#                     wherever this script runs) before issuing the request, and adds
#                     it as an "Authorization: Bearer <token>" header (in addition to
#                     any REQUEST_HEADERS). Requires OIDC_CLIENT_ID and OIDC_CLIENT_SECRET.
#   OIDC_CLIENT_ID    client_id for the client_credentials grant.
#   OIDC_CLIENT_SECRET  client_secret for the client_credentials grant.
#   OIDC_SCOPE        Optional scope parameter for the client_credentials grant.
set -o errexit
set -o nounset
set -o pipefail

ADDRESS="${ADDRESS}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH}"
EXPECTED_BODY="${EXPECTED_BODY:-}"
UNEXPECTED_BODY="${UNEXPECTED_BODY:-}"
EXPECTED_JSONRPC="${EXPECTED_JSONRPC:-}"
EXPECTED_JSON_ID="${EXPECTED_JSON_ID:-}"
EXPECTED_JSON_RESULT_TEXT="${EXPECTED_JSON_RESULT_TEXT:-}"
REQUEST_BODY="${REQUEST_BODY:-}"
REQUEST_HEADERS="${REQUEST_HEADERS:-}"
CONTENT_TYPE="${CONTENT_TYPE:-application/json}"
EXPECTED_STATUS="${EXPECTED_STATUS:-200}"
EXPECTED_STATUS_NOT="${EXPECTED_STATUS_NOT:-}"
PORT="${PORT:-443}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"
OIDC_TOKEN_URL="${OIDC_TOKEN_URL:-}"
OIDC_CLIENT_ID="${OIDC_CLIENT_ID:-}"
OIDC_CLIENT_SECRET="${OIDC_CLIENT_SECRET:-}"
OIDC_SCOPE="${OIDC_SCOPE:-}"

URL="https://${ADDRESS}:${PORT}${ROUTE_PATH}"
BODY_FILE=$(mktemp /tmp/aigw_agent_body.XXXXXX)
cleanup() { rm -f "${BODY_FILE}"; }
trap cleanup EXIT
# Set before print_result() is usable (curl_command is only known once build_curl_cmd runs).
CURL_CMD=""

# jq isn't bundled in curlimages/curl (the image used to run this script
# in-cluster), so string fields are extracted with grep/sed instead. Only
# matches simple "key": "value" pairs on flat/shallow single-line JSON, which
# is all this script needs (OIDC token responses, JSON-RPC test fixtures).
json_field() {
  local body="$1"
  local key="$2"
  printf '%s' "${body}" | grep -o "\"${key}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed -E 's/.*:[[:space:]]*"([^"]*)"$/\1/'
}

# Pure shell JSON string escaping (backslashes, double quotes, newlines) since
# jq isn't available in-cluster either. Uses awk rather than sed's classic
# ":a;N;$!ba" multi-line join idiom: BSD/macOS sed (used when this script
# runs directly on the host) silently emits nothing for single-line input in
# that idiom, while GNU/busybox sed (used in-cluster) does not, so it caused
# curl_command/body to come back empty only on from-host runs. Escaping is
# done character-by-character rather than via gsub, since gsub's replacement
# argument re-interprets backslashes/ampersands specially and would otherwise
# under-escape backslashes.
json_escape() {
  printf '%s' "$1" | awk '
    {
      line = $0
      out = ""
      n = length(line)
      for (i = 1; i <= n; i++) {
        c = substr(line, i, 1)
        if (c == "\\") out = out "\\\\"
        else if (c == "\"") out = out "\\\""
        else out = out c
      }
      printf "%s%s", (NR > 1 ? "\\n" : ""), out
    }
  '
}

print_result() {
  local success="$1"
  local code="$2"
  local body="$3"
  local attempt="$4"
  local error="${5:-}"
  cat <<EOF
{
  "success": ${success},
  "http_status": "${code:-000}",
  "expected_status": "${EXPECTED_STATUS}",
  "expected_status_not": "${EXPECTED_STATUS_NOT}",
  "method": "${METHOD}",
  "url": "${URL}",
  "retry_attempt": ${attempt},
  "max_retries": ${MAX_RETRIES},
  "curl_command": "$(json_escape "${CURL_CMD}")",
  "body": "$(json_escape "${body}")"$( [ -n "${error}" ] && printf ',\n  "error": "%s"' "$(json_escape "${error}")" )
}
EOF
}

if [ -n "${OIDC_TOKEN_URL}" ]; then
  TOKEN=""
  # A few retries (distinct from the main request retry loop below) absorb
  # transient connection blips (e.g. right after an OIDC provider rollout)
  # so a real invalid_client rejection isn't masked by one. `|| true`: under
  # `set -e`, a failing curl would otherwise abort the script with no output.
  for TOKEN_ATTEMPT in 1 2 3 4 5; do
    TOKEN_RESPONSE="$(curl -sk -m 30 \
      --data-urlencode "grant_type=client_credentials" \
      --data-urlencode "client_id=${OIDC_CLIENT_ID}" \
      --data-urlencode "client_secret=${OIDC_CLIENT_SECRET}" \
      ${OIDC_SCOPE:+--data-urlencode "scope=${OIDC_SCOPE}"} \
      "${OIDC_TOKEN_URL}" || true)"
    # `|| true`: json_field's internal grep returns non-zero when
    # TOKEN_RESPONSE has no access_token field (e.g. empty, or an error
    # response). Since this is a standalone `var="$(...)"` assignment, that
    # non-zero status would otherwise trip `errexit` right here, silently
    # killing the script with no output before the invalid_client check
    # below ever runs.
    TOKEN="$(json_field "${TOKEN_RESPONSE}" access_token || true)"
    [ -n "${TOKEN}" ] && break
    # invalid_client means the credentials were genuinely rejected: no point retrying.
    printf '%s' "${TOKEN_RESPONSE}" | grep -q '"error"[[:space:]]*:[[:space:]]*"invalid_client"' && break
    [ "${TOKEN_ATTEMPT}" -lt 5 ] && sleep 2
  done
  if [ -z "${TOKEN}" ]; then
    print_result false "" "" 0 "failed to obtain an OIDC access token from ${OIDC_TOKEN_URL}: ${TOKEN_RESPONSE}"
    exit 1
  fi
  REQUEST_HEADERS="$(printf '%s\nAuthorization:Bearer %s' "${REQUEST_HEADERS}" "${TOKEN}")"
fi

# Build the curl command as a single string, both to execute (via eval, as the
# other *_connectivity_test.sh scripts do) and to show verbatim in the JSON result.
build_curl_cmd() {
  local CMD="curl -sk -m 30 -o ${BODY_FILE} -w '%{http_code}' -X ${METHOD}"
  if [ -n "${REQUEST_BODY}" ]; then
    CMD="${CMD} -H 'Content-Type: ${CONTENT_TYPE}' --data '${REQUEST_BODY}'"
  fi
  if [ -n "${REQUEST_HEADERS}" ]; then
    while IFS= read -r h; do
      [ -n "${h}" ] && CMD="${CMD} -H '${h}'"
    done <<EOF
${REQUEST_HEADERS}
EOF
  fi
  CMD="${CMD} '${URL}'"
  echo "${CMD}"
}

CURL_CMD="$(build_curl_cmd)"

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

  if [ -n "${EXPECTED_JSONRPC}" ]; then
    [ "$(json_field "${body}" jsonrpc)" = "${EXPECTED_JSONRPC}" ] || return 1
  fi

  if [ -n "${EXPECTED_JSON_ID}" ]; then
    [ "$(json_field "${body}" id)" = "${EXPECTED_JSON_ID}" ] || return 1
  fi

  if [ -n "${EXPECTED_JSON_RESULT_TEXT}" ]; then
    [ "$(json_field "${body}" text)" = "${EXPECTED_JSON_RESULT_TEXT}" ] || return 1
  fi

  return 0
}

CODE=""
BODY=""
for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  > "${BODY_FILE}"
  CODE="$(eval "${CURL_CMD}" 2>/dev/null || echo 000)"
  BODY="$(cat "${BODY_FILE}" 2>/dev/null || echo '')"
  if response_matches "${CODE}" "${BODY}"; then
    print_result true "${CODE}" "${BODY}" "${ATTEMPT}"
    exit 0
  fi
  echo "attempt ${ATTEMPT}/${MAX_RETRIES}: status='${CODE}' body='${BODY}'" >&2
  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then
    sleep "${RETRY_DELAY}"
  fi
done

print_result false "${CODE}" "${BODY}" "${MAX_RETRIES}" "response did not match expectations after ${MAX_RETRIES} attempts"
exit 1
