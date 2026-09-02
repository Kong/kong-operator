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
#   MAX_RETRIES      (optional) Default: 180.
#   RETRY_DELAY      (optional) Default: 1.
set -eu

KAFKA_BOOTSTRAP="${KAFKA_BOOTSTRAP}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

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
LAST_ERR=""
while [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; do
  ATTEMPT=$((ATTEMPT + 1))

  # List first and only create what's missing. Previously this created every
  # topic on every attempt: once a topic already existed from an earlier
  # (partially successful) attempt, kafkactl's create call for it failed with
  # a "topic already exists" error, which the loop treated the same as a real
  # failure. That permanently poisoned every subsequent attempt (the list
  # verification below was only reached when ALL creates reported success),
  # so a single transient failure on the first attempt guaranteed the loop
  # burned through its entire retry budget and failed, no matter how quickly
  # Kafka actually became ready.
  LISTED=$(kafkactl -C /tmp/kafkactl.yml --context backend list topics 2>&1) || { LAST_ERR="list topics: ${LISTED}"; LISTED=""; }

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

  for TOPIC in ${MISSING}; do
    if ! OUT=$(kafkactl -C /tmp/kafkactl.yml --context backend \
        create topic "${TOPIC}" \
        --partitions 3 --replication-factor 3 2>&1); then
      LAST_ERR="create ${TOPIC}: ${OUT}"
    fi
  done

  if [ "${ATTEMPT}" -lt "${MAX_RETRIES}" ]; then sleep "${RETRY_DELAY}"; fi
done

# Emit the last kafkactl error as JSON-escaped text so a future failure is
# diagnosable instead of silently exhausting retries (see incident where this
# ran for the full retry budget with no visible cause).
ESCAPED_ERR=$(printf '%s' "${LAST_ERR}" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')
cat <<EOF
{
  "success": false,
  "error": "Failed to create/verify all topics after ${MAX_RETRIES} attempts",
  "last_error": "${ESCAPED_ERR}",
  "retry_attempt": ${ATTEMPT},
  "max_retries": ${MAX_RETRIES}
}
EOF
exit 1
