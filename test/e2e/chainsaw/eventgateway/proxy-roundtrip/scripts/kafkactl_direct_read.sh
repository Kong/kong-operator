#!/usr/bin/env bash
# Consume a topic directly from Kafka (bypassing keg) from inside the cluster.
# Spins up a disposable kafkactl pod via `kubectl run --rm`, pipes the inner
# script into it, and streams output back to the Chainsaw script step.
#
# Required env:
#   KAFKA_DIRECT    host:port of the Kafka bootstrap Service (bypasses gateway).
#   TOPIC           Kafka topic to consume.
#   HEADER_KEY      Header that must NOT appear on directly-consumed records.
#   NAMESPACE       Kubernetes namespace for the test pod.
set -euo pipefail

: "${KAFKA_DIRECT:?must be set}"
: "${TOPIC:?must be set}"
: "${HEADER_KEY:?must be set}"
: "${NAMESPACE:?must be set}"

POD_NAME="kafkactl-direct-$(date +%s)"
_STDERR=$(mktemp /tmp/kubectl-direct-stderr.XXXXXX)
_on_exit() { [ $? -ne 0 ] && cat "${_STDERR}" >&2; rm -f "${_STDERR}"; }
trap _on_exit EXIT

cat scripts/kafkactl_direct_read_inner.sh | kubectl run "${POD_NAME}" \
  --image=deviceinsight/kafkactl:latest \
  --rm -i --restart=Never \
  --env="KAFKA_DIRECT=${KAFKA_DIRECT}" \
  --env="TOPIC=${TOPIC}" \
  --env="HEADER_KEY=${HEADER_KEY}" \
  -n "${NAMESPACE}" \
  --command -- sh -s 2>"${_STDERR}"
