#!/bin/sh
# Runs in the traffic pod (onprem.sh traffic-start): produces a record
# through keg every TRAFFIC_INTERVAL seconds, each with its own connection,
# until /tmp/stop exists, printing "ok <n>" for each record acknowledged and
# "fail <n> <time> <error>" for each that isn't, then "stopped <n>". The pod
# keeps running afterwards, so that its log, and the topic, can be read.
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

# The topic is created before the traffic starts: a failure to create it isn't
# downtime.
until kafkactl -C /tmp/kafkactl.yml --context vc create topic "${TOPIC}" --partitions 3 --replication-factor 3 >/dev/null 2>&1 ||
  kafkactl -C /tmp/kafkactl.yml --context vc describe topic "${TOPIC}" >/dev/null 2>&1; do
  sleep 1
done
echo "topic ${TOPIC} ready"

n=0
while [ ! -f /tmp/stop ]; do
  n=$((n + 1))
  if out=$(kafkactl -C /tmp/kafkactl.yml --context vc produce "${TOPIC}" --value "seq-${n}" 2>&1); then
    echo "ok ${n}"
  else
    echo "fail ${n} $(date -u +%H:%M:%S) $(echo "${out}" | tr '\n' ' ')"
  fi
  sleep "${TRAFFIC_INTERVAL}"
done
echo "stopped ${n}"
exec sleep 3600
