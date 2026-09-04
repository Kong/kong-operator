#!/bin/bash
# Wait for a second AIGatewayDataPlaneCertificate to appear alongside an existing
# one -- the "blue-green" rotation registering a new Konnect certificate entity
# without removing the previous one. Fails if the original entity disappears
# before the new one shows up (it must not be removed before the rollout to the
# new certificate is confirmed complete).
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search in.
#   LABEL_SELECTOR: Label selector matching all certificates owned by the AIGatewayDataPlane.
#   CERT_A_NAME: Name of the certificate entity that existed before the rotation.
#   MAX_RETRIES: (optional) Number of attempts. Default: 90.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 2.

NAMESPACE="${NAMESPACE}"
LABEL_SELECTOR="${LABEL_SELECTOR}"
CERT_A_NAME="${CERT_A_NAME}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

NAMES_JSON="[]"

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  NAMES_JSON=$(kubectl get aigatewaydataplanecertificates -n "$NAMESPACE" -l "$LABEL_SELECTOR" -o json | \
    jq -c '[.items[].metadata.name]')

  FOUND_A=$(echo "$NAMES_JSON" | jq --arg a "$CERT_A_NAME" 'any(. == $a)')
  if [[ "$FOUND_A" != "true" ]]; then
    cat <<EOF
{
  "success": false,
  "error": "certificate $CERT_A_NAME was removed before the new one appeared",
  "names": $NAMES_JSON,
  "retry_attempt": $ATTEMPT
}
EOF
    exit 1
  fi

  COUNT=$(echo "$NAMES_JSON" | jq 'length')
  if [[ "$COUNT" -ge 2 ]]; then
    CERT_B_NAME=$(echo "$NAMES_JSON" | jq -r --arg a "$CERT_A_NAME" 'map(select(. != $a)) | .[0]')
    cat <<EOF
{
  "success": true,
  "cert_b_name": "$CERT_B_NAME",
  "names": $NAMES_JSON,
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES
}
EOF
    exit 0
  fi

  [[ "$ATTEMPT" -lt "$MAX_RETRIES" ]] && sleep "$RETRY_DELAY"
done

cat <<EOF
{
  "success": false,
  "error": "timed out waiting for a second AIGatewayDataPlaneCertificate to appear",
  "names": $NAMES_JSON,
  "max_retries": $MAX_RETRIES
}
EOF
exit 1
