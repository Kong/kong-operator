#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Runs aigw_chat_completion.sh from inside the cluster (the AIGatewayDataPlane
# ingress Service's cluster-DNS name is only reachable in-cluster), by piping
# it into a short-lived curl Pod's stdin, mirroring
# aigw_agent_request_from_cluster.sh / pod_connectivity_test_http.sh.
#
# Variables (from environment):
#   SCRIPT_PATH: (optional) Path to aigw_chat_completion.sh.
#                Default: ../../common/scripts/aigw_chat_completion.sh
#   NAMESPACE: Namespace the AIGatewayDataPlane / ingress Service and test Pod live in.
#   DP_SVC: Ingress Service name (typically <aigw-dp-name>-ingress).
#   CURL_IMAGE: (optional) Image for the in-cluster curl Pod. Default: curlimages/curl:latest.
#   ROUTE_PATH, MODEL_ALIAS, EXPECT_HEADER, EXPECTED_SUCCESS, REJECT_CONFIRMATIONS,
#   INCLUDE_HEADER, INCLUDE_BODY_MODEL, PORT, MAX_RETRIES, RETRY_DELAY: Forwarded
#     through as-is to aigw_chat_completion.sh (see its own env docs).

SCRIPT_PATH="${SCRIPT_PATH:-../../common/scripts/aigw_chat_completion.sh}"
NAMESPACE="${NAMESPACE}"
DP_SVC="${DP_SVC}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"

POD_NAME="aigw-chat-traffic-$(date +%s)-${RANDOM}"
ADDRESS="${DP_SVC}.${NAMESPACE}.svc"

cat "${SCRIPT_PATH}" | kubectl run "${POD_NAME}" \
  --image="${CURL_IMAGE}" --image-pull-policy=IfNotPresent \
  --rm -i --restart=Never \
  --env="ADDRESS=${ADDRESS}" \
  --env="ROUTE_PATH=${ROUTE_PATH}" \
  --env="MODEL_ALIAS=${MODEL_ALIAS}" \
  --env="EXPECT_HEADER=${EXPECT_HEADER:-X-Kong-LLM-Model}" \
  --env="EXPECTED_SUCCESS=${EXPECTED_SUCCESS:-true}" \
  --env="REJECT_CONFIRMATIONS=${REJECT_CONFIRMATIONS:-3}" \
  --env="INCLUDE_HEADER=${INCLUDE_HEADER:-true}" \
  --env="INCLUDE_BODY_MODEL=${INCLUDE_BODY_MODEL:-true}" \
  --env="PORT=${PORT:-443}" \
  --env="MAX_RETRIES=${MAX_RETRIES:-180}" \
  --env="RETRY_DELAY=${RETRY_DELAY:-1}" \
  -n "${NAMESPACE}" \
  -- sh -s 2>/dev/null | grep -v '^pod "'
