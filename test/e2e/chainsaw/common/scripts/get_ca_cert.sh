#!/usr/bin/env bash
# Retrieve the CA certificate stored in a ConfigMap during cert generation
# and output it as a base64-encoded JSON field for use in kafkactl TLS config.
#
# Required env:
#   NAMESPACE    Kubernetes namespace.
#   CA_CM_NAME   Name of the ConfigMap holding the CA cert (key: ca.crt).
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
CA_CM_NAME="${CA_CM_NAME}"

CA_CRT=$(kubectl -n "${NAMESPACE}" get configmap "${CA_CM_NAME}" \
  -o jsonpath='{.data.ca\.crt}' 2>/dev/null || true)

if [[ -z "${CA_CRT}" ]]; then
  cat <<EOF
{
  "success": false,
  "error": "CA cert not found in ConfigMap ${CA_CM_NAME} in namespace ${NAMESPACE}",
  "configmap": "${CA_CM_NAME}",
  "namespace": "${NAMESPACE}"
}
EOF
  exit 1
fi

CA_CERT_B64=$(printf '%s' "${CA_CRT}" | base64 | tr -d '\n')
cat <<EOF
{
  "success": true,
  "ca_cert_b64": "${CA_CERT_B64}",
  "configmap": "${CA_CM_NAME}",
  "namespace": "${NAMESPACE}"
}
EOF
