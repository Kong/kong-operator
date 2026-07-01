#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   GATEWAY_NAME: The name of the gateway.
#   NAMESPACE: The namespace to search for KongSNI resources.

GATEWAY_NAME="${GATEWAY_NAME}"
NAMESPACE="${NAMESPACE}"

output=$(kubectl get kongsnis.configuration.konghq.com -n "${NAMESPACE}" \
  -l "gateway-operator.konghq.com/hybrid-gateways-name=${GATEWAY_NAME}" \
  -l "gateway-operator.konghq.com/hybrid-gateways-namespace=${NAMESPACE}" \
  -o name)

if [[ -n "${output}" ]]; then
  echo "Unexpected KongSNI resources: ${output}"
  exit 1
fi
