#!/bin/sh
# Inner script executed inside a disposable kafkactl pod.
# Connects to a keg virtual cluster via SNI routing (TLS) and validates the
# docs scenario from https://developer.konghq.com/event-gateway/configure-sni-routing/:
#
#   1. List topics visible through the virtual cluster.
#   2. Assert every EXPECTED_TOPICS is present (namespace prefix hidden + shared topic).
#   3. Assert every UNEXPECTED_TOPICS is absent (namespace isolation).
#   4. Produce a record to PRODUCE_TOPIC and consume it back (data-path validation).
#
# Env vars provided by kubectl run --env:
#   GATEWAY_BOOTSTRAP    bootstrap-{dns_label}{sni_suffix}:{port}
#   CA_CERT_B64          Base64-encoded PEM CA certificate.
#   EXPECTED_TOPICS      Space-separated list of topics that MUST be visible.
#   UNEXPECTED_TOPICS    Space-separated list of topics that MUST NOT be visible.
#   PRODUCE_TOPIC        Topic to produce+consume through (must be in EXPECTED_TOPICS).
#   MAX_RETRIES          (optional) Default: 180.
#   RETRY_DELAY          (optional) Default: 1.
set -eu

GATEWAY_BOOTSTRAP="${GATEWAY_BOOTSTRAP}"
CA_CERT_B64="${CA_CERT_B64}"
EXPECTED_TOPICS="${EXPECTED_TOPICS}"
UNEXPECTED_TOPICS="${UNEXPECTED_TOPICS:-}"
PRODUCE_TOPIC="${PRODUCE_TOPIC}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Write CA certificate so kafkactl can verify the gateway's TLS cert.
printf '%s' "${CA_CERT_B64}" | base64 -d > /tmp/ca.crt

cat > /tmp/kafkactl.yml <<EOF
contexts:
  vc:
    brokers:
      - ${GATEWAY_BOOTSTRAP}
    tls:
      enabled: true
      ca: /tmp/ca.crt
      insecure: false
EOF

# --- Phase 1: topic listing + isolation ---
ATTEMPT=0
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  LISTED=$(kafkactl -C /tmp/kafkactl.yml --context vc \
    list topics 2>/dev/null || true)

  if [ -z "${LISTED}" ]; then
    if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  # Verify all expected topics are present.
  MISSING=""
  for TOPIC in ${EXPECTED_TOPICS}; do
    if ! echo "${LISTED}" | grep -qF "${TOPIC}"; then
      MISSING="${MISSING} ${TOPIC}"
    fi
  done

  if [ -n "${MISSING}" ]; then
    if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  # Verify isolation: unexpected topics must not appear.
  LEAKED=""
  for TOPIC in ${UNEXPECTED_TOPICS}; do
    if echo "${LISTED}" | grep -qF "${TOPIC}"; then
      LEAKED="${LEAKED} ${TOPIC}"
    fi
  done

  if [ -n "${LEAKED}" ]; then
    cat <<EOF
{
  "success": false,
  "error": "Namespace isolation violated: unexpected topics visible through VC",
  "leaked_topics": "${LEAKED}",
  "gateway_bootstrap": "${GATEWAY_BOOTSTRAP}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
    exit 1
  fi

  # Topic listing passed — break out for data-path phase.
  break
done

if [ "${ATTEMPT}" -ge "${MAX_RETRIES}" ]; then
  cat <<EOF
{
  "success": false,
  "error": "Expected topics not visible after ${MAX_RETRIES} attempts",
  "missing_topics": "${MISSING:-unknown}",
  "expected_topics": "${EXPECTED_TOPICS}",
  "gateway_bootstrap": "${GATEWAY_BOOTSTRAP}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
  exit 1
fi

# --- Phase 2: produce + consume (data-path validation) ---
RECORD_VALUE="sni-test-${ATTEMPT}-$(date +%s)"

PRODUCE_ATTEMPT=0
while [ "${PRODUCE_ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  PRODUCE_ATTEMPT=$((PRODUCE_ATTEMPT + 1))

  if ! kafkactl -C /tmp/kafkactl.yml --context vc \
      produce "${PRODUCE_TOPIC}" --value "${RECORD_VALUE}" >/dev/null 2>&1; then
    if [ "${PRODUCE_ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  if CONSUME_OUT=$(kafkactl -C /tmp/kafkactl.yml --context vc \
      consume "${PRODUCE_TOPIC}" --from-beginning --exit 2>/dev/null); then
    if echo "${CONSUME_OUT}" | grep -qF "${RECORD_VALUE}"; then
      cat <<EOF
{
  "success": true,
  "message": "Topic listing and data-path validation passed",
  "expected_topics": "${EXPECTED_TOPICS}",
  "produce_topic": "${PRODUCE_TOPIC}",
  "gateway_bootstrap": "${GATEWAY_BOOTSTRAP}",
  "retry_attempt": ${ATTEMPT},
  "produce_attempt": ${PRODUCE_ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
      exit 0
    fi
  fi

  if [ "${PRODUCE_ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
done

cat <<EOF
{
  "success": false,
  "error": "Produce+consume failed after ${MAX_RETRIES} attempts",
  "produce_topic": "${PRODUCE_TOPIC}",
  "gateway_bootstrap": "${GATEWAY_BOOTSTRAP}",
  "retry_attempt": ${ATTEMPT},
  "produce_attempt": ${PRODUCE_ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1

