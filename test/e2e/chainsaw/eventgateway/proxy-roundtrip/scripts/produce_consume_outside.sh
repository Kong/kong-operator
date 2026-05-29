#!/usr/bin/env bash
# Drive a produce + consume round-trip against the keg gateway from outside
# the cluster, using a kafkactl container scheduled on the host's Docker
# runtime. Asserts that the modify-headers consume policy injected the
# expected header on the way back out.
#
# Required env:
#   GATEWAY_ADDR      host:port of the gateway's bootstrap listener (LB IP + 19092).
#   TOPIC             Kafka topic name to produce/consume on.
#   HEADER_KEY        Header expected to be present on the consumed record.
#   HEADER_VALUE      Header value expected.
#   RECORD_VALUE      Record payload to produce.
set -euo pipefail

: "${GATEWAY_ADDR:?must be set}"
: "${TOPIC:?must be set}"
: "${HEADER_KEY:?must be set}"
: "${HEADER_VALUE:?must be set}"
: "${RECORD_VALUE:?must be set}"

IMG="deviceinsight/kafkactl:latest"
CFG=$(mktemp)
trap 'rm -f "${CFG}"' EXIT
cat > "${CFG}" <<EOF
contexts:
  vc:
    brokers:
      - ${GATEWAY_ADDR}
EOF

run() {
  docker run --rm -i \
    --network host \
    -v "${CFG}:/etc/kafkactl/config.yml:ro" \
    "${IMG}" --context vc "$@"
}

echo "[outside] get brokers"
run get brokers

echo "[outside] create topic ${TOPIC}"
run create topic "${TOPIC}" --partitions 3 --replication-factor 3 || true

echo "[outside] produce"
run produce "${TOPIC}" --value "${RECORD_VALUE}"

echo "[outside] consume (expect header)"
OUT=$(run consume "${TOPIC}" --print-headers --from-beginning --exit)
echo "${OUT}"

if ! grep -q "${HEADER_KEY}:${HEADER_VALUE}" <<<"${OUT}"; then
  echo "FAIL: expected header ${HEADER_KEY}:${HEADER_VALUE} not found in consumer output" >&2
  exit 1
fi
echo "[outside] header present, OK"
