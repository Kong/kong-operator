#!/usr/bin/env bash
# Write the CA certificate stored in a Secret (key ca.crt) to a temp file and
# print its path as JSON, for scripts that need to pass --cacert to curl.
# Extracted so every caller (host_connectivity_test_with_hostname_tls_check.sh
# callers included) shares one implementation instead of repeating the
# kubectl/base64/mktemp dance inline.
#
# Required env:
#   NAMESPACE       Kubernetes namespace the Secret lives in.
#   CA_SECRET_NAME  Name of the Secret holding the CA cert (key: ca.crt).
#
# Chainsaw usage: json_parse($stdout).ca_cert_path
# Caller owns cleanup, e.g.: trap 'rm -f "$CACERT_PATH"' EXIT
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE="${NAMESPACE}"
CA_SECRET_NAME="${CA_SECRET_NAME}"

CA_CERT_PATH="$(mktemp)"
kubectl -n "${NAMESPACE}" get secret "${CA_SECRET_NAME}" \
  -o jsonpath='{.data.ca\.crt}' | base64 --decode > "${CA_CERT_PATH}" 2>/dev/null || true

if [[ ! -s "${CA_CERT_PATH}" ]]; then
  rm -f "${CA_CERT_PATH}"
  jq -n --arg secret "${CA_SECRET_NAME}" --arg namespace "${NAMESPACE}" \
    '{success: false, error: "CA cert not found or empty in Secret \($secret) in namespace \($namespace)"}'
  exit 1
fi

jq -n --arg path "${CA_CERT_PATH}" '{success: true, ca_cert_path: $path}'
