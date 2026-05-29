#!/bin/sh
# Inner script executed inside a disposable kafkactl pod.
# Connects directly to Kafka (no TLS, in-cluster DNS) and pre-seeds all topics
# that the SNI routing validation steps will verify through the virtual clusters.
#
# Topics created (matching the docs scenario):
#   analytics_pageviews, analytics_clicks, analytics_orders  — analytics VC namespace
#   payments_transactions, payments_refunds, payments_orders  — payments VC namespace
#   user_actions                                              — shared additional topic
#
# Env vars provided by kubectl run --env:
#   KAFKA_BOOTSTRAP  Direct Kafka bootstrap server (e.g. kafka-bootstrap.<ns>.svc:9092).
#   MAX_RETRIES      (optional) Default: 60.
#   RETRY_DELAY      (optional) Default: 5.
set -eu

KAFKA_BOOTSTRAP="${KAFKA_BOOTSTRAP}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-5}"

cat > /tmp/kafkactl.yml <<EOF
contexts:
  backend:
    brokers:
      - ${KAFKA_BOOTSTRAP}
EOF

TOPICS="analytics_pageviews analytics_clicks analytics_orders \
        payments_transactions payments_refunds payments_orders \
        user_actions"

ATTEMPT=0
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  ALL_OK=true
  for TOPIC in ${TOPICS}; do
    if ! kafkactl -C /tmp/kafkactl.yml --context backend \
        create topic "${TOPIC}" \
        --partitions 3 --replication-factor 3 >/dev/null 2>&1; then
      ALL_OK=false
    fi
  done

  if [ "${ALL_OK}" = "true" ]; then
    # Verify topics are visible.
    LISTED=$(kafkactl -C /tmp/kafkactl.yml --context backend \
      list topics 2>/dev/null || true)
    MISSING=""
    for TOPIC in ${TOPICS}; do
      if ! echo "${LISTED}" | grep -qF "${TOPIC}"; then
        MISSING="${MISSING} ${TOPIC}"
      fi
    done

    if [ -z "${MISSING}" ]; then
      cat <<EOF
{
  "success": true,
  "message": "All topics created successfully",
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
  "error": "Failed to create/verify all topics after ${MAX_RETRIES} attempts",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
