#!/bin/bash
# Copy a field of a Kubernetes resource into a key of a (created or updated)
# Secret in the same namespace, e.g. to hand a CR's Konnect ID
# (status.id) to a later step that reads it from the Secret
# (see konnect_api_request_with_secret.sh). Retries until the field is set.
#
# Prints {"secret": "<namespace>/<name>", "key": "<key>", "value": <value>}.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'aigatewaycertificates.aiconfiguration.konghq.com').
#   RESOURCE_NAME: The resource name.
#   NAMESPACE: The namespace of the resource and the Secret.
#   FIELD: jq path of the field to copy (e.g. '.status.id').
#   SECRET_NAME: Name of the Secret to write.
#   SECRET_KEY: Key of the Secret to write the value to.

RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
NAMESPACE="${NAMESPACE}"
FIELD="${FIELD}"
SECRET_NAME="${SECRET_NAME}"
SECRET_KEY="${SECRET_KEY}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

VALUE="$(bash "${SCRIPT_DIR}/get_resource_field.sh" | jq -r '.value')"

if ! APPLY_OUTPUT="$(kubectl create secret generic "${SECRET_NAME}" -n "${NAMESPACE}" \
  --from-literal="${SECRET_KEY}=${VALUE}" --dry-run=client -o json | kubectl apply -f - 2>&1)"; then
  jq -n \
    --arg error "failed to write Secret ${NAMESPACE}/${SECRET_NAME}" \
    --arg kubectl_output "${APPLY_OUTPUT}" \
    '{error: $error, kubectl_output: $kubectl_output}'
  exit 1
fi

jq -n --arg secret "${NAMESPACE}/${SECRET_NAME}" --arg key "${SECRET_KEY}" --arg value "${VALUE}" \
  '{secret: $secret, key: $key, value: $value}'
