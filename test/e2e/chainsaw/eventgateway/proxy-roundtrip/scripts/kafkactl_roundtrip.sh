#!/usr/bin/env bash
# Produce+consume round-trip through the keg gateway from inside the cluster.
# Spins up a disposable kafkactl pod via `kubectl run --rm`, pipes the inner
# script into it, and streams output back to the Chainsaw script step.
#
# Required env:
#   GATEWAY_ADDR    host:port bootstrap address (LB IP + listener port).
#   TOPIC           Kafka topic name.
#   HEADER_KEY      Header expected on consumed records.
#   HEADER_VALUE    Expected header value.
#   RECORD_VALUE    Record payload to produce.
#   NAMESPACE       Kubernetes namespace for the test pod.
set -euo pipefail

: "${GATEWAY_ADDR:?must be set}"
: "${TOPIC:?must be set}"
: "${HEADER_KEY:?must be set}"
: "${HEADER_VALUE:?must be set}"
: "${RECORD_VALUE:?must be set}"
: "${NAMESPACE:?must be set}"

POD_NAME="kafkactl-roundtrip-$(date +%s)"
_STDERR=$(mktemp /tmp/kubectl-roundtrip-stderr.XXXXXX)
_on_exit() { [ $? -ne 0 ] && cat "${_STDERR}" >&2; rm -f "${_STDERR}"; }
trap _on_exit EXIT

cat scripts/kafkactl_roundtrip_inner.sh | kubectl run "${POD_NAME}" \
  --image=deviceinsight/kafkactl:latest \
  --rm -i --restart=Never \
  --env="GATEWAY_ADDR=${GATEWAY_ADDR}" \
  --env="TOPIC=${TOPIC}" \
  --env="HEADER_KEY=${HEADER_KEY}" \
  --env="HEADER_VALUE=${HEADER_VALUE}" \
  --env="RECORD_VALUE=${RECORD_VALUE}" \
  -n "${NAMESPACE}" \
  --command -- sh -s 2>"${_STDERR}"
