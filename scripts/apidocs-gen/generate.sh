#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."
CRD_REF_DOCS_BIN="$1"

# Create a temporary working directory where we'll copy the CRDs' definitions.
# It needs to be created in the repository tree so that crd-ref-docs can resolve Go module context.
WORK_DIR=$(mktemp -d -p "${SCRIPT_ROOT}")

# Cleanup temporary working directory on exit.
function cleanup {
  rm -rf "${WORK_DIR}"
  echo "Cleaned up temporary working directory: ${WORK_DIR}"
}
trap cleanup EXIT

# Resolve the path to the kubernetes-configuration module in Go modules' cache.
KUBERNETES_CONFIGURATION_CRDS_PACKAGE="github.com/kong/kubernetes-configuration"
KUBERNETES_CONFIGURATION_CRDS_VERSION=$(go list -m -f '{{ .Version }}' ${KUBERNETES_CONFIGURATION_CRDS_PACKAGE})
KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH="$(go env GOPATH)/pkg/mod/${KUBERNETES_CONFIGURATION_CRDS_PACKAGE}@${KUBERNETES_CONFIGURATION_CRDS_VERSION}"

# Copy the CRDs' definitions to the working directory.
# We're copying from the local ./api directory and the one in the kubernetes-configuration module.
cp -r "${SCRIPT_ROOT}/api" "${WORK_DIR}/kgo"
# Using --no-preserve=mode,ownership to avoid permission issues when deleting files copied from the modules' cache.
cp --no-preserve=mode,ownership -r "${KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH}/api" "${WORK_DIR}/kong"

# Ensure the output directory exists.
mkdir -p docs

set -x
${CRD_REF_DOCS_BIN} \
    --source-path="${WORK_DIR}" \
    --config="${SCRIPT_ROOT}/scripts/apidocs-gen/config.yaml" \
    --templates-dir="${SCRIPT_ROOT}/scripts/apidocs-gen/template" \
    --renderer=markdown \
    --output-path="${SCRIPT_ROOT}/docs/api-reference.md" \
    --max-depth=11
