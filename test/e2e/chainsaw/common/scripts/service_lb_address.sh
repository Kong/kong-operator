#!/usr/bin/env bash
# Wait for a LoadBalancer Service (e.g. an AIGatewayDataPlane's
# <name>-ingress or a KegDataPlane's <name>-kafka Service) to be assigned an
# external address and output the result as a JSON blob to stdout.
# Chainsaw captures the address via: json_parse($stdout).address
#
# Required env:
#   NAMESPACE     Namespace the Service lives in.
#   SERVICE_NAME  Name of the Service.
# Optional env:
#   MAX_RETRIES   Maximum retry attempts. Default: 180.
#   RETRY_DELAY   Seconds between retries. Default: 1.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
SERVICE_NAME="${SERVICE_NAME}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

ADDR=""

for ATTEMPT in $(seq 1 "${MAX_RETRIES}"); do
  IP=$(kubectl -n "${NAMESPACE}" get svc "${SERVICE_NAME}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)
  HOSTNAME=$(kubectl -n "${NAMESPACE}" get svc "${SERVICE_NAME}" \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)
  ADDR="${IP:-${HOSTNAME}}"
  if [[ -n "${ADDR}" ]]; then
    ADDR_TYPE="ip"
    [[ -z "${IP}" ]] && ADDR_TYPE="hostname"
    cat <<EOF
{
  "success": true,
  "address": "${ADDR}",
  "address_type": "${ADDR_TYPE}",
  "service": "${NAMESPACE}/${SERVICE_NAME}",
  "message": "Service ${NAMESPACE}/${SERVICE_NAME} was assigned LoadBalancer ${ADDR_TYPE} ${ADDR} on attempt ${ATTEMPT}/${MAX_RETRIES}",
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
  "address": null,
  "service": "${NAMESPACE}/${SERVICE_NAME}",
  "error": "Service ${NAMESPACE}/${SERVICE_NAME} never got a LoadBalancer address after ${MAX_RETRIES} attempts",
  "message": "Service ${NAMESPACE}/${SERVICE_NAME} never got a LoadBalancer address after ${MAX_RETRIES} attempts (${RETRY_DELAY}s apart)",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
