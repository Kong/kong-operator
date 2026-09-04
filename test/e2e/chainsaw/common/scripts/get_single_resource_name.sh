#!/bin/bash
# Look up a resource's name by label selector, for cases where the name isn't
# predictable (e.g. an automatically-provisioned certificate Secret or Konnect
# certificate entity). Retries since the resource may not have been created yet
# by the time this runs.
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   RESOURCE_TYPE: The Kubernetes resource type (e.g. 'secrets', 'aigatewaydataplanecertificates').
#   NAMESPACE: The namespace to search in.
#   LABEL_SELECTOR: Label selector matching exactly one resource.
#   MAX_RETRIES: (optional) Number of attempts. Default: 180.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

RESOURCE_TYPE="${RESOURCE_TYPE}"
NAMESPACE="${NAMESPACE}"
LABEL_SELECTOR="${LABEL_SELECTOR}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

KUBECTL_CMD="kubectl get $RESOURCE_TYPE -n $NAMESPACE -l $LABEL_SELECTOR -o json"
KUBECTL_OUTPUT=""
COUNT=0

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  if ! KUBECTL_OUTPUT=$(kubectl get "$RESOURCE_TYPE" -n "$NAMESPACE" -l "$LABEL_SELECTOR" -o json 2>&1); then
    cat <<EOF
{
  "error": "Failed to list $RESOURCE_TYPE",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "label_selector": "$LABEL_SELECTOR",
  "kubectl_command": "$KUBECTL_CMD",
  "kubectl_output": $(echo "$KUBECTL_OUTPUT" | jq -Rs .)
}
EOF
    exit 1
  fi

  COUNT=$(echo "$KUBECTL_OUTPUT" | jq '.items | length')
  if [[ "$COUNT" -eq 1 ]]; then
    RESOURCE_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r '.items[0].metadata.name')
    cat <<EOF
{
  "name": "$RESOURCE_NAME",
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
  "error": "expected exactly 1 $RESOURCE_TYPE, found $COUNT after $MAX_RETRIES attempts",
  "resource_type": "$RESOURCE_TYPE",
  "namespace": "$NAMESPACE",
  "label_selector": "$LABEL_SELECTOR",
  "names": $(echo "$KUBECTL_OUTPUT" | jq -c '[.items[].metadata.name]'),
  "max_retries": $MAX_RETRIES
}
EOF
exit 1
