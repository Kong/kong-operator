#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   GATEWAY_NAME: The name of the gateway.
#   NAMESPACE: The namespace to search for the KongCertificate resource.

GATEWAY_NAME="${GATEWAY_NAME}"
GATEWAY_NAMESPACE="${NAMESPACE}"

# Query for the KongCertificate name using labels.
if ! KUBECTL_OUTPUT=$(kubectl get kongcertificates.configuration.konghq.com -n "$GATEWAY_NAMESPACE" \
  -l "gateway-operator.konghq.com/hybrid-gateways-name=$GATEWAY_NAME" \
  -l "gateway-operator.konghq.com/hybrid-gateways-namespace=$GATEWAY_NAMESPACE" \
  -o json 2>&1); then
  cat <<EOF
{
  "error": "Failed to get KongCertificate resource",
  "gateway_name": "$GATEWAY_NAME",
  "gateway_namespace": "$GATEWAY_NAMESPACE",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

CERT_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r '.items[0].metadata.name // ""')

if [ -z "$CERT_NAME" ]; then
  cat <<EOF
{
  "error": "No KongCertificate found for Gateway",
  "gateway_name": "$GATEWAY_NAME",
  "gateway_namespace": "$GATEWAY_NAMESPACE",
  "available_certificates": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[] | {name: .metadata.name, labels: .metadata.labels}]')
}
EOF
  exit 1
fi

printf '{"certificate_name":"%s"}\n' "$CERT_NAME"
