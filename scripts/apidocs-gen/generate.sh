#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."
CRD_REF_DOCS_BIN="$1"

generate() {
  echo "INFO: generating API docs for ${1} package, output: ${2}"
  ${CRD_REF_DOCS_BIN} \
      --source-path="${SCRIPT_ROOT}${1}" \
      --config="${SCRIPT_ROOT}/scripts/apidocs-gen/config.yaml" \
      --templates-dir="${SCRIPT_ROOT}/scripts/apidocs-gen/template" \
      --renderer=markdown \
      --output-path="${SCRIPT_ROOT}${2}" \
      --max-depth=20
}

generate "/api/configuration" "/docs/configuration-api-reference.md"
  
generate "/api/konnect" "/docs/konnect-api-reference.md"

generate "/api/incubator" "/docs/incubator-api-reference.md"

generate "/api/gateway-operator" "/docs/gateway-operator-api-reference.md"

generate "/api" "/docs/all-api-reference.md"

# Post-process generated docs to map external SDK types to primitives
chmod +x "${SCRIPT_ROOT}/scripts/apidocs-gen/post-process-types.sh"
"${SCRIPT_ROOT}/scripts/apidocs-gen/post-process-types.sh"
