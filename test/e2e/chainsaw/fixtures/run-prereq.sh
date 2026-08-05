#!/usr/bin/env bash
# Generic dispatcher for chainsaw suite prerequisites.
#
# Always (idempotently) applies the common prerequisites shared by every suite
# (currently: a KongLicense resource), then runs the prerequisite script for
# the suite (test/e2e/chainsaw/fixtures/<DIRNAME>/prereq.sh) if it exists.
# No-ops for suites that have no prerequisites of their own, so a single CI
# step can call this for every matrix suite unconditionally.
#
# Required env:
#   DIRNAME             Suite/fixtures directory name (e.g. "aigateway"). Strictly validated.
# Optional env:
#   KONG_LICENSE_DATA   Raw Kong Enterprise license JSON. Treated as a secret: only ever
#                        read via env var, never echoed, logged, or passed as an argument.
#                        Skips KongLicense creation if unset.
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

case "${1:-}" in
  install|uninstall)
    ;;
  *)
    echo "[run-prereq] unknown action '${1:-}', must be 'install' or 'uninstall'" >&2
    exit 1
    ;;
esac

KONG_LICENSE_NAME="e2e-chainsaw-license"

case "$1" in
  install)
    if [ -n "${KONG_LICENSE_DATA:-}" ]; then
      echo "[run-prereq] applying KongLicense '${KONG_LICENSE_NAME}'"
      jq -n --arg name "${KONG_LICENSE_NAME}" --arg license "${KONG_LICENSE_DATA}" '{
        apiVersion: "configuration.konghq.com/v1alpha1",
        kind: "KongLicense",
        metadata: { name: $name },
        rawLicenseString: $license,
        enabled: true
      }' | kubectl apply -f - >/dev/null
    else
      echo "[run-prereq] KONG_LICENSE_DATA not set, skipping KongLicense creation"
    fi
    ;;
  uninstall)
    echo "[run-prereq] removing KongLicense '${KONG_LICENSE_NAME}' (if present)"
    kubectl delete konglicense "${KONG_LICENSE_NAME}" --ignore-not-found=true >/dev/null
    ;;
esac

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SUITE_SCRIPT="${SCRIPT_DIR}/${DIRNAME}/prereq.sh"

if [ -f "${SUITE_SCRIPT}" ]; then
  case "$1" in
    install)
      echo "[run-prereq] installing prerequisites for suite '${DIRNAME}'"
      ;;
    uninstall)
      echo "[run-prereq] uninstalling prerequisites for suite '${DIRNAME}'"
      ;;
  esac
  bash "${SUITE_SCRIPT}" "$1"
else
  echo "[run-prereq] no suite-specific prerequisites for suite '${DIRNAME}', skipping"
fi
