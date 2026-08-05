#!/usr/bin/env bash
# Wait for an AIGatewayDataPlane's ingress Service to be assigned an external
# LoadBalancer address and output the result as a JSON blob to stdout.
# Chainsaw captures the address via: json_parse($stdout).address
#
# Required env:
#   NAMESPACE     Namespace the AIGatewayDataPlane / ingress Service live in.
#   AIGW_DP_NAME  Name of the AIGatewayDataPlane (its ingress Service is <AIGW_DP_NAME>-ingress).
# Optional env:
#   MAX_RETRIES   Maximum retry attempts. Default: 180.
#   RETRY_DELAY   Seconds between retries. Default: 1.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
AIGW_DP_NAME="${AIGW_DP_NAME}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

SVC="${AIGW_DP_NAME}-ingress"
ADDR=""

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  IP=$(kubectl -n "${NAMESPACE}" get svc "${SVC}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)
  HOSTNAME=$(kubectl -n "${NAMESPACE}" get svc "${SVC}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
  ADDR="${IP:-${HOSTNAME}}"
  if [[ -n "${ADDR}" ]]; then
    cat <<EOF
{
  "success": true,
  "address": "${ADDR}",
  "service": "${SVC}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
    exit 0
  fi
  if [[ ${ATTEMPT} -lt ${MAX_RETRIES} ]]; then
    sleep "${RETRY_DELAY}"
  fi
done

cat <<EOF
{
  "success": false,
  "error": "Service ${SVC} never got a LoadBalancer address after ${MAX_RETRIES} attempts",
  "service": "${SVC}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
