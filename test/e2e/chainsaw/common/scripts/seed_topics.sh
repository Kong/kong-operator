#!/usr/bin/env bash
# Outer script: spins up a disposable kafkactl pod via kubectl run and
# pre-seeds all Kafka topics needed by the SNI routing validation steps.
# Connects directly to Kafka (no TLS, in-cluster DNS) via the bootstrap Service.
#
# Required env:
#   NAMESPACE    Kubernetes namespace for the test pod and Kafka Service.
set -o errexit
set -o nounset
set -o pipefail

: "${NAMESPACE:?must be set}"

KAFKA_BOOTSTRAP="kafka-bootstrap.${NAMESPACE}.svc:9092"
POD_NAME="kafkactl-seed-$(date +%s)"
SCRIPT_DIR="$(dirname "$0")"

cat "${SCRIPT_DIR}/seed_topics_inner.sh" | kubectl run "${POD_NAME}" \
  --image=deviceinsight/kafkactl:latest \
  --rm -i --restart=Never \
  --env="KAFKA_BOOTSTRAP=${KAFKA_BOOTSTRAP}" \
  -n "${NAMESPACE}" \
  --command -- sh -s 2>/dev/null | grep -v '^pod "'
