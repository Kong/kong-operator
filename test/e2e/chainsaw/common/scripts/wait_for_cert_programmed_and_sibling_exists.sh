#!/bin/bash
# Wait for an AIGatewayDataPlaneCertificate to reach Programmed=True (with a
# Konnect id assigned), and on the very same poll where that first happens,
# confirm a sibling AIGatewayDataPlaneCertificate still exists. Checking both
# in the same iteration, rather than in a later, separately-scheduled step,
# closes the race window where the Deployment's rollout could complete and
# remove the sibling between two separate checks.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace to search in.
#   CERT_NAME: Name of the AIGatewayDataPlaneCertificate to wait for Programmed=True.
#   SIBLING_CERT_NAME: Name of the AIGatewayDataPlaneCertificate that must still
#     exist once CERT_NAME becomes Programmed.
#   MAX_RETRIES: (optional) Number of attempts. Default: 180.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

NAMESPACE="${NAMESPACE}"
CERT_NAME="${CERT_NAME}"
SIBLING_CERT_NAME="${SIBLING_CERT_NAME}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  PROGRAMMED=$(kubectl get aigatewaydataplanecertificates "$CERT_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null || true)
  ID=$(kubectl get aigatewaydataplanecertificates "$CERT_NAME" -n "$NAMESPACE" \
    -o jsonpath='{.status.id}' 2>/dev/null || true)

  if [[ "$PROGRAMMED" == "True" && -n "$ID" ]]; then
    # Same iteration: check the sibling right now, before anything else runs.
    if ! kubectl get aigatewaydataplanecertificates "$SIBLING_CERT_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
      cat <<EOF
{
  "success": false,
  "error": "$SIBLING_CERT_NAME was already removed by the time $CERT_NAME became Programmed",
  "retry_attempt": $ATTEMPT
}
EOF
      exit 1
    fi
    cat <<EOF
{
  "success": true,
  "cert_name": "$CERT_NAME",
  "sibling_cert_name": "$SIBLING_CERT_NAME",
  "id": "$ID",
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
  "error": "timed out waiting for $CERT_NAME to become Programmed",
  "max_retries": $MAX_RETRIES
}
EOF
exit 1
