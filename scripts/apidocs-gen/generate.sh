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

for dir in $(find "${SCRIPT_ROOT}/api" -mindepth 1 -maxdepth 1 -type d); do
  if [[ "${dir}" == "${SCRIPT_ROOT}/api/common" || "${dir}" == "${SCRIPT_ROOT}/api/test" ]]; then
    continue
  fi
  output_file="/docs/$(basename ${dir})-api-reference.md"
  generate "${dir#${SCRIPT_ROOT}}" "${output_file}"
done

generate "/api" "/docs/all-api-reference.md"
