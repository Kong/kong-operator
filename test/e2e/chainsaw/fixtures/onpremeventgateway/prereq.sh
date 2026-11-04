#!/usr/bin/env bash
# Prerequisites for the onpremeventgateway and eventgateway chainsaw suites: one
# Kafka cluster in the namespace kafka-shared, used by every test of both. On-prem
# tests keep their topics and consumer groups apart with the prefix of their
# virtual cluster (onpremeventgateway/onprem-shared/entities.yaml), Konnect tests
# with topic names of their own.
#
# The resources are those of common/_step_templates/apply-assert-kafka-cluster.yaml.
#
# Optional env:
#   KAFKA_TIMEOUT   Rollout timeout for the Kafka StatefulSet. Default: 300s.
set -o errexit
set -o nounset
set -o pipefail

NAMESPACE=kafka-shared
KAFKA_TIMEOUT="${KAFKA_TIMEOUT:-300s}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/../../common/_step_templates/apply-assert-kafka-cluster.yaml"

case $1 in
  install)
    kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
    yq '{"apiVersion": "v1", "kind": "List", "items": [.spec.try[] | select(has("apply")) | .apply.resource]}' "${TEMPLATE}" |
      kubectl apply -n "${NAMESPACE}" -f -
    kubectl -n "${NAMESPACE}" rollout status statefulset/kafka --timeout="${KAFKA_TIMEOUT}"
    ;;
  uninstall)
    kubectl delete namespace "${NAMESPACE}" --ignore-not-found=true
    ;;
  *)
    echo "unknown action '$1', must be 'install' or 'uninstall'" >&2
    exit 1
    ;;
esac
