#!/usr/bin/env bash
# Prerequisites for the aigateway chainsaw suite: the shared mock LLM stack
# (Ollama + a DB-less Kong proxy doing api-key auth). Applies the manifests in
# this directory and waits for both deployments to become ready.
#
# Optional env:
#   OLLAMA_TIMEOUT      Rollout timeout for the Ollama deployment. Default: 900s.
#   KONG_MOCK_TIMEOUT   Rollout timeout for the kong-mock deployment. Default: 180s.
set -o errexit
set -o nounset
set -o pipefail

OLLAMA_TIMEOUT="${OLLAMA_TIMEOUT:-900s}"
KONG_MOCK_TIMEOUT="${KONG_MOCK_TIMEOUT:-180s}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Print the mock stack's state and logs when a rollout fails. The CI job runs
# this script on its own, so without this the job log shows only the rollout
# timeout error; the cause stays in the artifact zip.
dump_mock_state() {
  echo "[prereq] mock stack state:" >&2
  kubectl -n kong-aigateway-mock get deploy,pods -o wide >&2 || true
  kubectl -n kong-aigateway-mock describe pods >&2 || true
  kubectl -n kong-aigateway-mock get events --sort-by=.lastTimestamp >&2 || true
  for deploy in ollama kong-mock; do
    echo "[prereq] last log lines from deploy/${deploy}, without transfer progress:" >&2
    # Ollama prints one progress line per tick while pulling a model: 3000 of
    # them hide the few lines that explain a failure.
    kubectl -n kong-aigateway-mock logs "deploy/${deploy}" --all-containers --tail=2000 2>&1 \
      | tr '\r' '\n' \
      | grep -avE 'pulling |verifying |writing |pulling manifest' \
      | tail -n 40 >&2 || true
  done
}

wait_for_rollout() {
  local name="$1"
  local timeout="$2"
  kubectl -n kong-aigateway-mock rollout status "deploy/${name}" --timeout="${timeout}" || {
    dump_mock_state
    exit 1
  }
}

case $1 in
  install)
    # Namespace first, then the rest of the manifests in this directory.
    kubectl apply -f "${SCRIPT_DIR}/00-namespace.yaml"
    kubectl apply -f "${SCRIPT_DIR}"

    # kong-mock only reads its declarative config at startup: stamp the pod
    # template with the config's checksum so that a changed config rolls the
    # pods on a cluster that already runs the mock (a no-op otherwise).
    KONG_MOCK_CONFIG_CHECKSUM="$(cksum < "${SCRIPT_DIR}/20-kong-mock-config.yaml" | cut -d' ' -f1)"
    kubectl -n kong-aigateway-mock patch deploy/kong-mock --type merge \
      -p "{\"spec\":{\"template\":{\"metadata\":{\"annotations\":{\"konghq.com/kong-mock-config-checksum\":\"${KONG_MOCK_CONFIG_CHECKSUM}\"}}}}}"

    wait_for_rollout ollama "${OLLAMA_TIMEOUT}"
    wait_for_rollout kong-mock "${KONG_MOCK_TIMEOUT}"
    ;;
  uninstall)
    kubectl delete --ignore-not-found=true -f "${SCRIPT_DIR}"
    kubectl delete --ignore-not-found=true -f "${SCRIPT_DIR}/00-namespace.yaml"
    ;;
  *)
    echo "unknown action '$1', must be 'install' or 'uninstall'" >&2
    exit 1
    ;;
esac
