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
#   MAX_RETRIES     (optional) Maximum retry attempts. Default: 60.
#   RETRY_DELAY     (optional) Seconds between retries. Default: 5.
set -eu

GATEWAY_ADDR="${GATEWAY_ADDR}"
TOPIC="${TOPIC}"
HEADER_KEY="${HEADER_KEY}"
HEADER_VALUE="${HEADER_VALUE}"
RECORD_VALUE="${RECORD_VALUE}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-5}"

cat > /tmp/kafkactl.yml <<EOF
contexts:
  vc:
    brokers:
      - ${GATEWAY_ADDR}
EOF

# Retry loop: retry until produce succeeds and the consumed record carries the
# expected header, or retries are exhausted.
ATTEMPT=0
LAST_OUT=""
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  # Create topic (idempotent; ignore errors if already exists or not ready).
  kafkactl -C /tmp/kafkactl.yml --context vc \
    create topic "${TOPIC}" --partitions 3 --replication-factor 3 >/dev/null 2>&1 || true

  # Produce; skip to next attempt if gateway is not yet ready.
  if ! kafkactl -C /tmp/kafkactl.yml --context vc \
      produce "${TOPIC}" --value "${RECORD_VALUE}" >/dev/null 2>&1; then
    if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  # Consume and check for the injected header.
  if LAST_OUT=$(kafkactl -C /tmp/kafkactl.yml --context vc \
      consume "${TOPIC}" --print-headers --from-beginning --exit 2>/dev/null); then
    if echo "${LAST_OUT}" | grep -q "${HEADER_KEY}:${HEADER_VALUE}"; then
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
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
