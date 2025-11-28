#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

# This script post-processes generated API reference docs to replace links to
# external SDK types with their primitive equivalents, so the docs do not
# reference missing type definitions.
#
# It operates in-place on the generated markdown files under ./docs/.

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."

# Candidate docs to process (some may not exist in all builds)
DOC_FILES=(
  "${SCRIPT_ROOT}/docs/konnect-api-reference.md"
  "${SCRIPT_ROOT}/docs/all-api-reference.md"
  "${SCRIPT_ROOT}/docs/configuration-api-reference.md"
  "${SCRIPT_ROOT}/docs/gateway-operator-api-reference.md"
  "${SCRIPT_ROOT}/docs/incubator-api-reference.md"
)

for f in "${DOC_FILES[@]}"; do
  if [[ -f "${f}" ]]; then
    echo "INFO: post-processing ${f}"
    # Map external types to primitives for readability and to avoid dead anchors.
    # AuthType -> string
    sed -i -E 's/_\[AuthType\]\(#authtype\)_/_string_/g' "${f}"
    # CreateControlPlaneRequestClusterType -> string
    sed -i -E 's/_\[CreateControlPlaneRequestClusterType\]\(#createcontrolplanerequestclustertype\)_/_string_/g' "${f}"
    # ControlPlaneClusterType -> string
    sed -i -E 's/_\[ControlPlaneClusterType\]\(#controlplaneclustertype\)_/_string_/g' "${f}"
    # ProxyURL -> object (array forms keep the trailing " array")
    sed -i -E 's/_\[ProxyURL\]\(#proxyurl\) array_/_object array_/g' "${f}"
    sed -i -E 's/_\[ProxyURL\]\(#proxyurl\)_/_object_/g' "${f}"
  fi
done