#!/usr/bin/env bash
# Prerequisites for the aigateway chainsaw suite: the shared mock LLM stack
# (Ollama + a DB-less Kong proxy doing api-key auth). Applies the manifests in
# this directory and waits for both deployments to become ready.
#
# Optional env:
#   OLLAMA_TIMEOUT      Rollout timeout for the Ollama deployment. Default: 600s.
#   KONG_MOCK_TIMEOUT   Rollout timeout for the kong-mock deployment. Default: 180s.
set -o errexit
set -o nounset
set -o pipefail

OLLAMA_TIMEOUT="${OLLAMA_TIMEOUT:-600s}"
KONG_MOCK_TIMEOUT="${KONG_MOCK_TIMEOUT:-180s}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Namespace first, then the rest of the manifests in this directory.
kubectl apply -f "${SCRIPT_DIR}/00-namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}"

kubectl -n kong-aigateway-mock rollout status deploy/ollama --timeout="${OLLAMA_TIMEOUT}"
kubectl -n kong-aigateway-mock rollout status deploy/kong-mock --timeout="${KONG_MOCK_TIMEOUT}"
