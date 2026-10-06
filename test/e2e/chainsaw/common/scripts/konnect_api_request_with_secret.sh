#!/bin/bash
# Send a request to the Konnect API, like konnect_api_request.sh, with the keys
# of a Kubernetes Secret available to the request BODY. Each key is exported as
# SECRET_<KEY>, upper-cased with non-alphanumerics replaced by "_", so BODY can
# read it via $ENV, e.g. a certificate: '{name: "x", cert: $ENV.SECRET_TLS_CRT}'.
#
# Prints what konnect_api_request.sh prints.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   SECRET_NAME: (optional) Name of the Secret. When empty, the request is sent
#     as is.
#   NAMESPACE: (required with SECRET_NAME) Namespace of the Secret.
#   All the variables of konnect_api_request.sh (KONNECT_TOKEN, KONNECT_URL,
#   API_PATH, METHOD, BODY, EXPECTED_STATUS, ...).

SECRET_NAME="${SECRET_NAME:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -n "${SECRET_NAME}" ]]; then
  NAMESPACE="${NAMESPACE}"
  if ! SECRET_JSON="$(kubectl get secret "${SECRET_NAME}" -n "${NAMESPACE}" -o json 2>&1)"; then
    jq -n \
      --arg error "cannot read Secret ${NAMESPACE}/${SECRET_NAME}" \
      --arg kubectl_output "${SECRET_JSON}" \
      '{error: $error, kubectl_output: $kubectl_output}'
    exit 1
  fi
  while IFS=$'\t' read -r KEY VALUE; do
    export "SECRET_${KEY}=$(base64 -d <<<"${VALUE}")"
  done < <(jq -r '.data // {} | to_entries[]
    | [(.key | ascii_upcase | gsub("[^A-Z0-9]"; "_")), .value] | @tsv' <<<"${SECRET_JSON}")
fi

exec bash "${SCRIPT_DIR}/konnect_api_request.sh"
