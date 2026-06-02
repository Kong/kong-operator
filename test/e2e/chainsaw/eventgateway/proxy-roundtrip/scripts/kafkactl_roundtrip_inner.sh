#!/bin/sh
# Runs inside the kafkactl pod (piped via stdin by kafkactl_roundtrip.sh).
# Produces a record through the keg gateway and consumes it back, asserting
# that the modify-headers consume policy injected the expected header.
# Retries until success or MAX_RETRIES attempts are exhausted.
#
# Env vars (set by kubectl run --env):
#   GATEWAY_ADDR    host:port bootstrap address (LB IP + listener port).
#   TOPIC           Kafka topic name.
#   HEADER_KEY      Header expected on every consumed record.
#   HEADER_VALUE    Expected header value.
#   RECORD_VALUE    Record payload to produce.
#   MAX_RETRIES     (optional) Maximum retry attempts. Default: 180.
#   RETRY_DELAY     (optional) Seconds between retries. Default: 1.
set -eu

GATEWAY_ADDR="${GATEWAY_ADDR}"
TOPIC="${TOPIC}"
HEADER_KEY="${HEADER_KEY}"
HEADER_VALUE="${HEADER_VALUE}"
RECORD_VALUE="${RECORD_VALUE}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

cat > /tmp/kafkactl.yml <<EOF
contexts:
  vc:
    brokers:
      - ${GATEWAY_ADDR}
EOF

# Retry loop: retry until produce succeeds and the consumed record carries the
# expected header, or retries are exhausted.
ATTEMPT=0
LAST_ERROR=""
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  # Create topic (idempotent; ignore errors if already exists or not ready).
  kafkactl -C /tmp/kafkactl.yml --context vc \
    create topic "${TOPIC}" --partitions 3 --replication-factor 3 >/dev/null 2>&1 || true

  # Produce; capture output for diagnostics on failure.
  PRODUCE_OUT=""
  if ! PRODUCE_OUT=$(kafkactl -C /tmp/kafkactl.yml --context vc \
      produce "${TOPIC}" --value "${RECORD_VALUE}" 2>&1); then
    LAST_ERROR="produce failed: ${PRODUCE_OUT}"
    if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  # Consume and check for the injected header.
  CONSUME_OUT=""
  if CONSUME_OUT=$(kafkactl -C /tmp/kafkactl.yml --context vc \
      consume "${TOPIC}" --print-headers --from-beginning --exit 2>&1); then
    if echo "${CONSUME_OUT}" | grep -q "${HEADER_KEY}:${HEADER_VALUE}"; then
      cat <<EOF
{
  "success": true,
  "message": "Header ${HEADER_KEY}:${HEADER_VALUE} found in consumed record",
  "topic": "${TOPIC}",
  "gateway_addr": "${GATEWAY_ADDR}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
      exit 0
    fi
    LAST_ERROR="consume output (header not found): ${CONSUME_OUT}"
  else
    LAST_ERROR="consume failed: ${CONSUME_OUT}"
  fi

  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
done

cat <<EOF
{
  "success": false,
  "error": "Header ${HEADER_KEY}:${HEADER_VALUE} not found after ${MAX_RETRIES} attempts",
  "topic": "${TOPIC}",
  "gateway_addr": "${GATEWAY_ADDR}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES},
  "last_error": "${LAST_ERROR}"
}
EOF
exit 1
