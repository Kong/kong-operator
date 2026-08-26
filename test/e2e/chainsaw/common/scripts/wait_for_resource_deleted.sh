#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: Kubernetes resource type (e.g. hpa, deployment, service).
#   RESOURCE_NAME: Name of the resource to wait for deletion.
#   NAMESPACE:     Namespace of the resource.
#   TIMEOUT:       (Optional) Timeout in seconds. Default: 300.

TIMEOUT="${TIMEOUT:-300}"

kubectl wait --for=delete "${RESOURCE_TYPE}/${RESOURCE_NAME}" \
  -n "${NAMESPACE}" \
  --timeout="${TIMEOUT}s"
