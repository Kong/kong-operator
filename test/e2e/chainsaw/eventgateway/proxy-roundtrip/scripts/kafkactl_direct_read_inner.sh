#!/bin/sh
# Runs inside the kafkactl pod (piped via stdin by kafkactl_direct_read.sh).
# Consumes a topic directly from Kafka (bypassing keg) and asserts that the
# modify-headers policy is NOT present — proving it is gateway-only.
#
# Env vars (set by kubectl run --env):
#   KAFKA_DIRECT    host:port of the Kafka bootstrap Service (bypasses gateway).
#   TOPIC           Kafka topic to consume.
#   HEADER_KEY      Header that must NOT appear on directly-consumed records.
#   MAX_RETRIES     (optional) Maximum retry attempts. Default: 180.
#   RETRY_DELAY     (optional) Seconds between retries. Default: 1.
set -eu

KAFKA_DIRECT="${KAFKA_DIRECT}"
TOPIC="${TOPIC}"
HEADER_KEY="${HEADER_KEY}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

cat > /tmp/kafkactl.yml <<EOF
contexts:
  direct:
    brokers:
      - ${KAFKA_DIRECT}
EOF

# Retry loop: wait for the topic (created by the roundtrip step) to appear.
ATTEMPT=0
TOPIC_FOUND=0
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))
  if kafkactl -C /tmp/kafkactl.yml --context direct \
       get topics 2>&1 | grep -q "^${TOPIC}[[:space:]]"; then
    TOPIC_FOUND=1
    break
  fi
  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
done

if [ "${TOPIC_FOUND}" -eq 0 ]; then
  cat <<EOF
{
  "success": false,
  "error": "Topic ${TOPIC} not found after ${MAX_RETRIES} attempts",
  "kafka_direct": "${KAFKA_DIRECT}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
  exit 1
fi

# Consume directly from Kafka (bypassing keg).
OUT=$(kafkactl -C /tmp/kafkactl.yml --context direct \
  consume "${TOPIC}" --print-headers --from-beginning --exit 2>&1 || echo "")

if echo "${OUT}" | grep -q "${HEADER_KEY}:"; then
  cat <<EOF
{
  "success": false,
  "error": "Header ${HEADER_KEY} found on direct read; gateway-only policy leaked",
  "topic": "${TOPIC}",
  "kafka_direct": "${KAFKA_DIRECT}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES},
  "consume_output": "${OUT}"
}
EOF
  exit 1
fi

cat <<EOF
{
  "success": true,
  "message": "Direct read has no injected header ${HEADER_KEY} (gateway-only policy confirmed)",
  "topic": "${TOPIC}",
  "kafka_direct": "${KAFKA_DIRECT}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 0
