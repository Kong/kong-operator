#!/usr/bin/env bash
# Drive a chat-completions request through an AIGatewayDataPlane and verify the
# gateway processed it: HTTP 200 and the model header present.
#
# Assumes ADDRESS is directly reachable from wherever this script runs: either
# the chainsaw test-runner host (e.g. an external LoadBalancer address), or an
# in-cluster curl Pod when piped through aigw_chat_completion_from_cluster.sh
# (e.g. the ingress Service's cluster-DNS name), mirroring
# aigw_agent_request.sh / aigw_agent_request_from_cluster.sh. Emits a JSON
# result object to stdout (success or failure) and exits non-zero (failing
# the chainsaw step) unless a matching response is observed within the retry
# budget.
#
# Required env:
#   ADDRESS       Host or IP to connect to (cluster-DNS name or external address).
#   ROUTE_PATH    Route path configured on the model (e.g. /aigw11/chat).
#   MODEL_ALIAS   Model alias to send.
#
# Optional env:
#   EXPECT_HEADER Response header proving AI Gateway processed the request.
#                 Default: X-Kong-LLM-Model.
#   EXPECTED_SUCCESS
#                 "true" (default): succeed once a 200 with EXPECT_HEADER is seen.
#                 "false": the alias is expected to NOT resolve to a model
#                 (e.g. a value absent from the selector's `values`). Succeeds
#                 once REJECT_CONFIRMATIONS consecutive attempts fail to
#                 produce a 200-with-header, guarding against a transient
#                 blip while the route is still propagating.
#   REJECT_CONFIRMATIONS
#                 Only used when EXPECTED_SUCCESS='false'. Number of
#                 *consecutive* non-matches required before concluding the
#                 alias is genuinely unmatched. Default: 3. Symmetric with the
#                 stale-success protection in aigw_agent_request.sh: a lone
#                 transient blip can't prematurely pass the check.
#   INCLUDE_HEADER
#                 "true" (default): send MODEL_ALIAS via the X-Kong-LLM-Model
#                 request header. Set "false" to omit it. AI Gateway accepts
#                 the model alias from more than one source at once (header,
#                 body, path), so leaving an unrelated source enabled can mask
#                 a broken selector under test with a false positive — set
#                 this "false" when isolating a route.model selector that is
#                 NOT `headerParam` (e.g. bodyParam, pathParam).
#   INCLUDE_BODY_MODEL
#                 "true" (default): send MODEL_ALIAS via the top-level "model"
#                 field of the JSON request body (the OpenAI-format
#                 convention). Set "false" to omit it for the same reason as
#                 INCLUDE_HEADER — isolate a route.model selector that is NOT
#                 `bodyParam` on the "model" field (e.g. headerParam, pathParam).
#   PORT          Ingress Service port. Default: 443.
#   MAX_RETRIES   Retry attempts. Default: 180.
#   RETRY_DELAY   Seconds between retries. Default: 1.
set -o errexit
set -o nounset
set -o pipefail

ADDRESS="${ADDRESS}"
ROUTE_PATH="${ROUTE_PATH}"
MODEL_ALIAS="${MODEL_ALIAS}"
EXPECT_HEADER="${EXPECT_HEADER:-X-Kong-LLM-Model}"
EXPECTED_SUCCESS="${EXPECTED_SUCCESS:-true}"
REJECT_CONFIRMATIONS="${REJECT_CONFIRMATIONS:-3}"
INCLUDE_HEADER="${INCLUDE_HEADER:-true}"
INCLUDE_BODY_MODEL="${INCLUDE_BODY_MODEL:-true}"
PORT="${PORT:-443}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

URL="https://${ADDRESS}:${PORT}${ROUTE_PATH}/chat/completions"
BODY_FILE=$(mktemp /tmp/aigw_chat_body.XXXXXX)
HEADER_FILE=$(mktemp /tmp/aigw_chat_headers.XXXXXX)
cleanup() { rm -f "${BODY_FILE}" "${HEADER_FILE}"; }
trap cleanup EXIT
# Set before print_result() is usable (curl_command is only known once build_curl_cmd runs).
CURL_CMD=""

if [ "${INCLUDE_BODY_MODEL}" = "true" ]; then
  REQUEST_BODY="{\"model\":\"${MODEL_ALIAS}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: Kong AI Gateway works.\"}]}"
else
  REQUEST_BODY="{\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: Kong AI Gateway works.\"}]}"
fi

# Pure shell JSON string escaping (backslashes, double quotes, newlines) since
# jq isn't available in the curlimages/curl image this script runs in-cluster
# under. Mirrors aigw_agent_request.sh's json_escape.
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
  local header_value="$4"
  local attempt="$5"
  local error="${6:-}"
  cat <<EOF
{
  "success": ${success},
  "http_status": "${code:-000}",
  "expected_header": "${EXPECT_HEADER}",
  "header_value": "$(json_escape "${header_value}")",
  "expected_success": "${EXPECTED_SUCCESS}",
  "model_alias": "${MODEL_ALIAS}",
  "url": "${URL}",
  "retry_attempt": ${attempt},
  "max_retries": ${MAX_RETRIES},
  "curl_command": "$(json_escape "${CURL_CMD}")",
  "body": "$(json_escape "${body}")"$( [ -n "${error}" ] && printf ',\n  "error": "%s"' "$(json_escape "${error}")" )
}
EOF
}

# Build the curl command as a single string, both to execute (via eval, as
# the other *_connectivity_test.sh scripts do) and to show verbatim in the
# JSON result for debugging.
build_curl_cmd() {
  local CMD="curl -sk -m 60 -o ${BODY_FILE} -D ${HEADER_FILE} -w '%{http_code}' -X POST"
  if [ "${INCLUDE_HEADER}" = "true" ]; then
    CMD="${CMD} -H 'X-Kong-LLM-Model: ${MODEL_ALIAS}'"
  fi
  CMD="${CMD} -H 'Content-Type: application/json' --data '${REQUEST_BODY}' '${URL}'"
  echo "${CMD}"
}

# Extracts the first value of a response header from HEADER_FILE (may contain
# multiple header blocks across TLS/redirect round-trips; the last one wins).
response_header_value() {
  local name="$1"
  grep -i "^${name}:" "${HEADER_FILE}" 2>/dev/null | tail -1 | cut -d' ' -f2- | tr -d '\r'
}

response_matches() {
  local code="$1"
  local header_value="$2"
  [ "${code}" = "200" ] && [ -n "${header_value}" ]
}

CODE=""
BODY=""
HEADER_VALUE=""
REJECTIONS=0
CURL_CMD="$(build_curl_cmd)"
for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  MATCHED=false
  > "${BODY_FILE}"
  > "${HEADER_FILE}"
  CODE="$(eval "${CURL_CMD}" 2>/dev/null || echo 000)"
  BODY="$(cat "${BODY_FILE}" 2>/dev/null || echo '')"
  # `|| true`: response_header_value's grep returns non-zero when the header
  # is absent (expected on every rejection-path response, e.g. a 503 with no
  # X-Kong-LLM-Model). Since this is a standalone `var="$(...)"` assignment,
  # that non-zero status would otherwise trip `errexit` right here, silently
  # killing the script before the match check below ever runs.
  HEADER_VALUE="$(response_header_value "${EXPECT_HEADER}" || true)"
  response_matches "${CODE}" "${HEADER_VALUE}" && MATCHED=true

  if [ "${EXPECTED_SUCCESS}" = "false" ]; then
    if [ "${MATCHED}" = "false" ]; then
      REJECTIONS=$((REJECTIONS + 1))
      [ "${REJECTIONS}" -ge "${REJECT_CONFIRMATIONS}" ] && { print_result true "${CODE}" "${BODY}" "${HEADER_VALUE}" "${ATTEMPT}"; exit 0; }
    else
      # A stale match (e.g. an old pod still serving during a rollout) breaks the
      # streak: only REJECT_CONFIRMATIONS *consecutive* non-matches conclude success.
      REJECTIONS=0
    fi
  else
    [ "${MATCHED}" = "true" ] && { print_result true "${CODE}" "${BODY}" "${HEADER_VALUE}" "${ATTEMPT}"; exit 0; }
  fi

  echo "attempt ${ATTEMPT}/${MAX_RETRIES}: matched=${MATCHED} status='${CODE}' rejections=${REJECTIONS}/${REJECT_CONFIRMATIONS} ${EXPECT_HEADER}='${HEADER_VALUE}'" >&2
  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then
    sleep "${RETRY_DELAY}"
  fi
done

if [ "${EXPECTED_SUCCESS}" = "false" ]; then
  print_result false "${CODE}" "${BODY}" "${HEADER_VALUE}" "${MAX_RETRIES}" "expected the alias to stay unmatched ${REJECT_CONFIRMATIONS} consecutive times, but never reached that streak within ${MAX_RETRIES} attempts (last streak: ${REJECTIONS})"
else
  print_result false "${CODE}" "${BODY}" "${HEADER_VALUE}" "${MAX_RETRIES}" "no 200 with ${EXPECT_HEADER} after ${MAX_RETRIES} attempts"
fi
exit 1
