#!/usr/bin/env bash
# Assert that the ACL policy blocks produce to blocked-topic through the gateway.
# Creates blocked-topic directly on Kafka, then attempts to produce through keg
# and expects an authorization error.
#
# Required env:
#   GATEWAY_ADDR    host:port bootstrap address (LB IP + listener port).
#   KAFKA_DIRECT    host:port of the Kafka bootstrap Service (bypasses gateway).
#   NAMESPACE       Kubernetes namespace for the test pod.
set -euo pipefail

: "${GATEWAY_ADDR:?must be set}"
: "${KAFKA_DIRECT:?must be set}"
: "${NAMESPACE:?must be set}"

POD_NAME="kafkactl-acl-blocked-$(date +%s)"
_STDERR=$(mktemp /tmp/kubectl-acl-blocked-stderr.XXXXXX)
_on_exit() { [ $? -ne 0 ] && cat "${_STDERR}" >&2; rm -f "${_STDERR}"; }
trap _on_exit EXIT

cat scripts/kafkactl_acl_blocked_inner.sh | kubectl run "${POD_NAME}" \
  --image=deviceinsight/kafkactl:latest \
  --rm -i --restart=Never \
  --env="GATEWAY_ADDR=${GATEWAY_ADDR}" \
  --env="KAFKA_DIRECT=${KAFKA_DIRECT}" \
  -n "${NAMESPACE}" \
  --command -- sh -s 2>"${_STDERR}"
