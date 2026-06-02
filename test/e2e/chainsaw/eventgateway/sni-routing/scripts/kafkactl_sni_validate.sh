#!/usr/bin/env bash
# Outer script: spins up a disposable kafkactl pod via kubectl run,
# connects to a keg virtual cluster via SNI routing (TLS), and validates
# the visible topics match the expected set (namespace isolation + shared topics).
#
# The --command flag is required because deviceinsight/kafkactl has
# ENTRYPOINT ["kafkactl"]; without it, sh would be passed as args to kafkactl.
#
# Required env:
#   LB_IP              LoadBalancer external IP (e.g. 10.96.1.5).
#   DNS_LABEL          Virtual cluster DNS label (e.g. analytics or payments).
#   CA_CERT_B64        Base64-encoded PEM CA certificate.
#   EXPECTED_TOPICS    Space-separated list of topics that MUST be visible.
#   UNEXPECTED_TOPICS  Space-separated list of topics that MUST NOT be visible.
#   PRODUCE_TOPIC      Topic to produce+consume through for data-path validation.
#   NAMESPACE          Kubernetes namespace for the test pod.
set -o errexit
set -o nounset
set -o pipefail

: "${LB_IP:?must be set}"
: "${DNS_LABEL:?must be set}"
: "${CA_CERT_B64:?must be set}"
: "${EXPECTED_TOPICS:?must be set}"
: "${PRODUCE_TOPIC:?must be set}"
: "${NAMESPACE:?must be set}"

UNEXPECTED_TOPICS="${UNEXPECTED_TOPICS:-}"

# Compute bootstrap address: replace dots with dashes and use sslip.io.
IP_DASHES="${LB_IP//./-}"
GATEWAY_BOOTSTRAP="bootstrap-${DNS_LABEL}.${IP_DASHES}.sslip.io:19092"

POD_NAME="kafkactl-sni-$(date +%s)"

cat scripts/kafkactl_sni_validate_inner.sh | kubectl run "${POD_NAME}" \
  --image=deviceinsight/kafkactl:latest \
  --rm -i --restart=Never \
  --env="GATEWAY_BOOTSTRAP=${GATEWAY_BOOTSTRAP}" \
  --env="CA_CERT_B64=${CA_CERT_B64}" \
  --env="EXPECTED_TOPICS=${EXPECTED_TOPICS}" \
  --env="UNEXPECTED_TOPICS=${UNEXPECTED_TOPICS}" \
  --env="PRODUCE_TOPIC=${PRODUCE_TOPIC}" \
  -n "${NAMESPACE}" \
  --command -- sh -s 2>/dev/null | grep -v '^pod "'
