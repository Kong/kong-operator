#!/usr/bin/env bash
# Generic dispatcher for chainsaw suite prerequisites.
#
# Runs the prerequisite script for a suite (test/e2e/chainsaw/fixtures/<DIRNAME>/prereq.sh)
# if it exists. No-ops for suites that have no prerequisites, so a single CI step can call
# this for every matrix suite unconditionally.
#
# Required env:
#   DIRNAME   Suite/fixtures directory name (e.g. "aigateway"). Strictly validated.
set -o errexit
set -o nounset
set -o pipefail

: "${DIRNAME:?DIRNAME must be set, e.g. DIRNAME=aigateway}"

# Whitelist: lowercase alphanumerics with internal dashes only. Forbids "/", ".", "..",
# whitespace and shell metacharacters, so DIRNAME can only name a direct child of this
# directory (no path traversal, no injection).
if ! printf '%s' "${DIRNAME}" | grep -Eq '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$'; then
  echo "[run-prereq] invalid DIRNAME '${DIRNAME}': must match ^[a-z0-9]([a-z0-9-]*[a-z0-9])?\$" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SUITE_SCRIPT="${SCRIPT_DIR}/${DIRNAME}/prereq.sh"

if [ -f "${SUITE_SCRIPT}" ]; then
  echo "[run-prereq] applying prerequisites for suite '${DIRNAME}'"
  bash "${SUITE_SCRIPT}"
else
  echo "[run-prereq] no prerequisites for suite '${DIRNAME}', skipping"
fi
