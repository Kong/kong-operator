#!/bin/bash

# This script adapts auto-generated api-reference.md to the requirements of
# developer.konghq.com:
#   - adds a title section
#   - turns vale linter off for the whole document
#   - replaces description placeholders with include directives (only if files exist)

# Usage: post-process-for-konghq.sh <input_file> <output_file> <docs_repo_path>
#   input_file: Path to the auto-generated CRD reference document
#   output_file: Path where the processed document should be written (in docs repo)
#   docs_repo_path: Path to the developer.konghq.com repository root
#
# Example:
#   ./scripts/apidocs-gen/post-process-for-konghq.sh docs/all-api-reference.md path-to-developer.konghq.com-repo/app/operator/reference/custom-resources.md path-to-developer.konghq.com-repo

set -o errexit
set -o nounset
set -o pipefail

# Check if required arguments are provided
if [[ $# -ne 3 ]]; then
    echo "Error: Missing required arguments"
    echo "Usage: $0 <input_file> <output_file> <docs_repo_path>"
    echo ""
    echo "Arguments:"
    echo "  input_file     Path to the auto-generated CRD reference document"
    echo "  output_file    Path where the processed document should be written (in docs repo)"
    echo "  docs_repo_path Path to the developer.konghq.com repository root"
    echo ""
    echo "Example:"
    echo "  $0 docs/all-api-reference.md path-to-developer.konghq.com-repo/app/operator/reference/custom-resources.md path-to-developer.konghq.com-repo"
    exit 1
fi

SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/../.."
CRD_REF_DOC="${1}"
POST_PROCESSED_DOC="${2}"
DOCS_REPO_PATH="${3}"

# Add a title and turn the vale linter off
cat > "${POST_PROCESSED_DOC}" <<'EOF'
---
title: "Custom resource definitions"
description: "Explore schemas of the available Custom Resources for {{ site.operator_product_name }}"
content_type: reference
layout: reference
products:
  - operator
breadcrumbs:
  - /operator/
---
<!-- vale off -->
EOF

# Add the generated doc content
cat "${CRD_REF_DOC}" >> "${POST_PROCESSED_DOC}"

# Turn the linter back on
echo "<!-- vale on -->" >> "${POST_PROCESSED_DOC}"

SED=sed
if [[ $(uname -s) == "Darwin" ]]; then
  if gsed --version 2>&1 >/dev/null ; then
    SED=gsed
  else
    echo "GNU sed is required on macOS. You can install it via Homebrew with 'brew install gnu-sed'."
    exit 1
  fi
fi

# Replace all description placeholders with proper include directives
# But only if the include file actually exists
while IFS= read -r line; do
  if [[ $line =~ \<!--\ (.*)\ description\ placeholder\ --\> ]]; then
    placeholder_name="${BASH_REMATCH[1]}"
    # Convert spaces to underscores for the filename
    filename_part="${placeholder_name// /_}"
    # Check if the include file exists in the docs repository
    include_file="${DOCS_REPO_PATH}/app/_includes/k8s/crd-ref/${filename_part}_description.md"
    # Replace with include directive only if file exists, otherwise keep placeholder
    if [[ -f "${include_file}" ]]; then
      ${SED} -i \
        "s/<!-- ${placeholder_name} description placeholder -->/{% include k8s\/crd-ref\/${filename_part}_description.md kong_version=page.kong_version %}/" \
        "${POST_PROCESSED_DOC}"
    fi
  fi
done < "${POST_PROCESSED_DOC}"
