#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Runs aigw_agent_request.sh from inside the cluster (the AIGatewayDataPlane
# ingress Service's cluster-DNS name is only reachable in-cluster), by piping
# it into a short-lived curl Pod's stdin, mirroring pod_connectivity_test_http.sh.
#
# Variables (from environment):
#   SCRIPT_PATH: (optional) Path to aigw_agent_request.sh.
#                Default: ../../common/scripts/aigw_agent_request.sh
#   NAMESPACE: Namespace the AIGatewayDataPlane / ingress Service and test Pod live in.
#   DP_SVC: Ingress Service name (typically <aigw-dp-name>-ingress).
#   CURL_IMAGE: (optional) Image for the in-cluster curl Pod. Default: curlimages/curl:latest.
#   METHOD, ROUTE_PATH, PORT, REQUEST_HEADERS, REQUEST_BODY, CONTENT_TYPE,
#   EXPECTED_STATUS, EXPECTED_STATUS_NOT, EXPECTED_BODY, UNEXPECTED_BODY,
#   EXPECTED_JSONRPC, EXPECTED_JSON_ID, EXPECTED_JSON_RESULT_TEXT, MAX_RETRIES,
#   RETRY_DELAY, OIDC_TOKEN_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_SCOPE,
#   EXPECTED_SUCCESS: Forwarded through as-is to aigw_agent_request.sh (see its
#     own env docs).

SCRIPT_PATH="${SCRIPT_PATH:-../../common/scripts/aigw_agent_request.sh}"
NAMESPACE="${NAMESPACE}"
DP_SVC="${DP_SVC}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"

POD_NAME="aigw-agent-traffic-$(date +%s)-${RANDOM}"
ADDRESS="${DP_SVC}.${NAMESPACE}.svc"

cat "${SCRIPT_PATH}" | kubectl run "${POD_NAME}" \
  --image="${CURL_IMAGE}" --image-pull-policy=IfNotPresent \
  --rm -i --restart=Never \
  --env="ADDRESS=${ADDRESS}" \
  --env="METHOD=${METHOD}" \
  --env="ROUTE_PATH=${ROUTE_PATH}" \
  --env="PORT=${PORT:-443}" \
  --env="REQUEST_HEADERS=${REQUEST_HEADERS:-}" \
  --env="REQUEST_BODY=${REQUEST_BODY:-}" \
  --env="CONTENT_TYPE=${CONTENT_TYPE:-application/json}" \
  --env="EXPECTED_STATUS=${EXPECTED_STATUS:-200}" \
  --env="EXPECTED_STATUS_NOT=${EXPECTED_STATUS_NOT:-}" \
  --env="EXPECTED_BODY=${EXPECTED_BODY:-}" \
  --env="UNEXPECTED_BODY=${UNEXPECTED_BODY:-}" \
  --env="EXPECTED_JSONRPC=${EXPECTED_JSONRPC:-}" \
  --env="EXPECTED_JSON_ID=${EXPECTED_JSON_ID:-}" \
  --env="EXPECTED_JSON_RESULT_TEXT=${EXPECTED_JSON_RESULT_TEXT:-}" \
  --env="MAX_RETRIES=${MAX_RETRIES:-180}" \
  --env="RETRY_DELAY=${RETRY_DELAY:-1}" \
  --env="OIDC_TOKEN_URL=${OIDC_TOKEN_URL:-}" \
  --env="OIDC_CLIENT_ID=${OIDC_CLIENT_ID:-}" \
  --env="OIDC_CLIENT_SECRET=${OIDC_CLIENT_SECRET:-}" \
  --env="OIDC_SCOPE=${OIDC_SCOPE:-}" \
  --env="EXPECTED_SUCCESS=${EXPECTED_SUCCESS:-true}" \
  -n "${NAMESPACE}" \
  -- sh -s 2>/dev/null | grep -v '^pod "'
