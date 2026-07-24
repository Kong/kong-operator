#!/bin/sh
# Runs inside the kafkactl pod (piped via stdin by kafkactl_acl_blocked.sh).
# 1. Creates blocked-topic directly on Kafka (bypassing the gateway).
# 2. Retries producing to blocked-topic through the gateway until the ACL
#    policy returns an authorization error, confirming enforcement.
#
# Produce success → ACL not yet propagated to data plane → retry.
# "not authorized" error → ACL enforced → exit 0.
# Any other produce failure → gateway not ready → retry.
#
# Env vars (set by kubectl run --env):
#   GATEWAY_ADDR    host:port bootstrap address (LB IP + listener port).
#   KAFKA_DIRECT    host:port of the Kafka bootstrap Service (bypasses gateway).
#   MAX_RETRIES     (optional) Maximum retry attempts. Default: 180.
#   RETRY_DELAY     (optional) Seconds between retries. Default: 1.
set -eu

GATEWAY_ADDR="${GATEWAY_ADDR}"
KAFKA_DIRECT="${KAFKA_DIRECT}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

cat > /tmp/kafkactl.yml <<EOF
contexts:
  direct:
    brokers:
      - ${KAFKA_DIRECT}
  vc:
    brokers:
      - ${GATEWAY_ADDR}
EOF

# Create blocked-topic directly on Kafka (idempotent; ignore errors).
kafkactl -C /tmp/kafkactl.yml --context direct \
  create topic blocked-topic --partitions 3 --replication-factor 3 >/dev/null 2>&1 || true

# Retry loop: wait until the gateway denies writes to blocked-topic.
ATTEMPT=0
LAST_ERR=""
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  LAST_ERR=""
  if LAST_ERR=$(kafkactl -C /tmp/kafkactl.yml --context vc \
      produce blocked-topic --value "test message" 2>&1); then
    # Produce succeeded — ACL not yet enforced, retry.
    if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
    continue
  fi

  if echo "${LAST_ERR}" | grep -qi "not authorized"; then
    cat <<EOF
{
  "success": true,
  "message": "ACL policy correctly blocked produce to blocked-topic",
  "gateway_addr": "${GATEWAY_ADDR}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
    exit 0
  fi

  # Other failure (connection error, not ready, etc.) — retry.
  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
done

cat <<EOF
{
  "success": false,
  "error": "produce to blocked-topic was not blocked by ACL after ${MAX_RETRIES} attempts",
  "last_error": "${LAST_ERR}",
  "gateway_addr": "${GATEWAY_ADDR}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
