#!/bin/bash
# Wait for a Deployment's pod-template annotation to change away from a known
# "before" value -- used to prove a spec change (e.g. a referenced Secret's
# content being rotated) actually triggered a new rollout, rather than just
# asserting the Deployment is healthy again (which would also be true if
# nothing had rolled at all).
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace of the Deployment.
#   DEPLOYMENT_NAME: The name of the Deployment.
#   ANNOTATION_KEY: The pod-template annotation key to watch (spec.template.metadata.annotations).
#   BEFORE_VALUE: The annotation's value before the change; waits until it differs from this
#     (and is non-empty).
#   MAX_RETRIES: (optional) Number of attempts. Default: 180.
#   RETRY_DELAY: (optional) Seconds between attempts. Default: 1.

NAMESPACE="${NAMESPACE}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME}"
ANNOTATION_KEY="${ANNOTATION_KEY}"
BEFORE_VALUE="${BEFORE_VALUE}"
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

AFTER_VALUE=""

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  AFTER_VALUE=$(kubectl get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" -o json | \
    jq -r --arg key "$ANNOTATION_KEY" '.spec.template.metadata.annotations[$key] // ""')

  if [[ -n "$AFTER_VALUE" && "$AFTER_VALUE" != "$BEFORE_VALUE" ]]; then
    cat <<EOF
{
  "success": true,
  "before": "$BEFORE_VALUE",
  "after": "$AFTER_VALUE",
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
  "error": "annotation did not change after $MAX_RETRIES attempts",
  "annotation_key": "$ANNOTATION_KEY",
  "before": "$BEFORE_VALUE",
  "after": "$AFTER_VALUE",
  "max_retries": $MAX_RETRIES
}
EOF
exit 1
