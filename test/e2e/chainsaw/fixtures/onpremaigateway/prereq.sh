#!/usr/bin/env bash
# Prerequisites for the onpremaigateway chainsaw suite.
#
# The suite reuses the shared mock LLM upstream (Ollama + a DB-less Kong proxy
# doing api-key auth) applied by the aigateway suite's prerequisites, so this
# script simply delegates to it. The mock is cluster-scoped and idempotent, so
# running both suites (in either order) shares one mock stack.
#
# Optional env (forwarded to the aigateway prereq script):
#   OLLAMA_TIMEOUT      Rollout timeout for the Ollama deployment. Default: 600s.
#   KONG_MOCK_TIMEOUT   Rollout timeout for the kong-mock deployment. Default: 180s.
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

case $1 in
  install)
    bash "${SCRIPT_DIR}/../aigateway/prereq.sh" install
    ;;
  uninstall)
    bash "${SCRIPT_DIR}/../aigateway/prereq.sh" uninstall
    ;;
  *)
    echo "unknown action '$1', must be 'install' or 'uninstall'" >&2
    exit 1
    ;;
esac
