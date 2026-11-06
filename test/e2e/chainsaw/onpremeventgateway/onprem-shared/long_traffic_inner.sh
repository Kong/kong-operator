#!/bin/sh
# Runs in the long-lived traffic pod (onprem.sh long-traffic-start): one
# kafkactl producer session produces a record through keg every
# TRAFFIC_INTERVAL seconds, read from a pipe, and one kafkactl consumer
# session follows the topic, both kept for the whole test, as long-lived Kafka
# clients do, the consumer resuming from its committed offsets when its
# connection drops. The producer stops once /tmp/stop exists: the pipe closes, and
# its exit code, in /tmp/producer.exit, tells whether every record was
# acknowledged. /tmp/produced counts the records written to the pipe, and
# /tmp/consumed holds what the consumer read.
#
# Env vars:
#   GATEWAY_ADDR      host:port bootstrap address of the Kafka Service of keg.
#   TOPIC             Kafka topic the records are produced to.
#   TRAFFIC_INTERVAL  Seconds between two records.
set -u

cat >/tmp/kafkactl.yml <<EOF
contexts:
  vc:
    brokers:
      - ${GATEWAY_ADDR}
    requestTimeout: 10s
EOF

until kafkactl -C /tmp/kafkactl.yml --context vc create topic "${TOPIC}" --partitions 3 --replication-factor 3 >/dev/null 2>&1 ||
  kafkactl -C /tmp/kafkactl.yml --context vc describe topic "${TOPIC}" >/dev/null 2>&1; do
  sleep 1
done

# A consumer whose connection drops, e.g. once its keg pod is replaced,
# resumes from the offsets its consumer group committed, as a Kafka consumer
# does: /tmp/consumer.restarts counts how many times it had to.
(
  restarts=0
  while :; do
    kafkactl -C /tmp/kafkactl.yml --context vc consume "${TOPIC}" --group long-lived --from-beginning --print-headers >>/tmp/consumed 2>>/tmp/consumer.log &
    echo "$!" >/tmp/consumer.pid
    wait "$!"
    restarts=$((restarts + 1))
    echo "${restarts}" >/tmp/consumer.restarts
    sleep 1
  done
) &

# A kafkactl consumer session can hang on a dropped connection instead of
# ending: one that reads nothing for STALL_SECONDS is killed, so that it resumes
# from the committed offsets as above. Records come every TRAFFIC_INTERVAL
# seconds, so a working consumer never stays still that long while they do.
STALL_SECONDS=20
(
  last=-1
  still=0
  while :; do
    sleep 1
    size=$(wc -c </tmp/consumed 2>/dev/null || echo 0)
    if [ "${size}" = "${last}" ]; then still=$((still + 1)); else still=0; fi
    last=${size}
    if [ "${still}" -ge "${STALL_SECONDS}" ]; then
      echo "$(date -u +%H:%M:%S) no record for ${STALL_SECONDS}s: restarting the consumer" >>/tmp/consumer.log
      kill -KILL "$(cat /tmp/consumer.pid)" 2>/dev/null
      still=0
    fi
  done
) &

(
  n=0
  while [ ! -f /tmp/stop ]; do
    n=$((n + 1))
    echo "seq-${n}"
    echo "${n}" >/tmp/produced
    sleep "${TRAFFIC_INTERVAL}"
  done
) | kafkactl -C /tmp/kafkactl.yml --context vc produce "${TOPIC}" >/tmp/producer.log 2>&1
echo $? >/tmp/producer.exit
exec sleep 3600
