#!/usr/bin/env bash
set -Eeuo pipefail

# Drops the advisories listed in hack/govulncheck-exclusions.json from a govulncheck SARIF file, so
# the code-scanning upload agrees with the exclusions make govulncheck already applies.
# Usage: govulncheck-sarif-with-excludes.sh <input.sarif> <output.sarif>

SCRIPT_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly SCRIPT_ROOT
EXCLUSIONS="${SCRIPT_ROOT}/hack/govulncheck-exclusions.json"
readonly EXCLUSIONS

if [[ "$#" -ne 2 ]]; then
  echo "usage: $(basename "$0") <input.sarif> <output.sarif>" >&2
  exit 2
fi

in="$1"
out="$2"

if [[ ! -f "${in}" ]]; then
  echo "no such SARIF file: ${in}" >&2
  exit 2
fi

excluded="$(jq -c 'map(.id)' "${EXCLUSIONS}")"

jq --argjson excluded "${excluded}" '
  .runs[].results |= map(select(.ruleId as $id | ($excluded | index($id)) | not))
' "${in}" > "${out}"
