#!/bin/bash
# Assert that a Kong resource's gateway-operator.konghq.com/hybrid-routes annotation contains
# exactly the expected set of HTTPRoute references.
#
# The annotation value is a comma-separated list of "namespace/name" HTTPRoute refs (CSV). This
# script builds the expected CSV value from EXPECTED_ROUTES, normalizes both the actual and
# expected values (dedup + sort, so ordering doesn't matter) and asserts they match exactly -
# catching both missing entries (not shared/not cleaned up) and unexpected extra entries
# (stale entries left behind, or a route's entry bleeding into the wrong resource).
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace where the resource is located.
#   RESOURCE_TYPE: The full resource type, e.g. kongservices.configuration.konghq.com.
#   RESOURCE_NAME: The name of the resource to check.
#   EXPECTED_ROUTES: Comma-separated list of "namespace/name" HTTPRoute refs expected to be the
#     exact set present in the annotation. Use "" (empty string) to assert the annotation is
#     absent/empty (e.g. no route references the resource anymore).
#   ANNOTATION_KEY: (Optional) Annotation key to check. Default: gateway-operator.konghq.com/hybrid-routes.
#   RETRY_COUNT: (Optional) Number of retries. Default: 60.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.

NAMESPACE="${NAMESPACE}"
RESOURCE_TYPE="${RESOURCE_TYPE}"
RESOURCE_NAME="${RESOURCE_NAME}"
EXPECTED_ROUTES="${EXPECTED_ROUTES:-}"
ANNOTATION_KEY="${ANNOTATION_KEY:-gateway-operator.konghq.com/hybrid-routes}"
RETRY_COUNT="${RETRY_COUNT:-60}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Build the normalized (deduped, sorted) expected CSV value from the input list.
normalize_csv() {
  echo "$1" | tr ',' '\n' | sed '/^$/d' | sort -u | paste -sd ',' -
}

EXPECTED_SORTED=$(normalize_csv "$EXPECTED_ROUTES")

ATTEMPT=0
while [[ $ATTEMPT -lt $RETRY_COUNT ]]; do
  ATTEMPT=$((ATTEMPT + 1))

  if ! RESOURCE_JSON=$(kubectl get "$RESOURCE_TYPE" "$RESOURCE_NAME" -n "$NAMESPACE" -o json 2>&1); then
    if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
      cat <<EOF
{
  "match": false,
  "error": "failed to get resource after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE",
  "kubectl_output": $(echo "$RESOURCE_JSON" | jq -Rs .)
}
EOF
      exit 1
    fi
    sleep "$RETRY_DELAY"
    continue
  fi

  ACTUAL_RAW=$(echo "$RESOURCE_JSON" | jq -r --arg key "$ANNOTATION_KEY" '.metadata.annotations[$key] // ""')
  ACTUAL_SORTED=$(normalize_csv "$ACTUAL_RAW")

  if [[ "$ACTUAL_SORTED" == "$EXPECTED_SORTED" ]]; then
    cat <<EOF
{
  "match": true,
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE",
  "annotation_key": "$ANNOTATION_KEY",
  "actual": "$ACTUAL_RAW",
  "expected": "$EXPECTED_SORTED",
  "retry_attempt": $ATTEMPT
}
EOF
    exit 0
  fi

  if [[ $ATTEMPT -eq $RETRY_COUNT ]]; then
    cat <<EOF
{
  "match": false,
  "error": "annotation does not match expected set of routes after $RETRY_COUNT attempts",
  "resource_type": "$RESOURCE_TYPE",
  "resource_name": "$RESOURCE_NAME",
  "namespace": "$NAMESPACE",
  "annotation_key": "$ANNOTATION_KEY",
  "actual_raw": "$ACTUAL_RAW",
  "actual_sorted": "$ACTUAL_SORTED",
  "expected_sorted": "$EXPECTED_SORTED",
  "retry_attempt": $ATTEMPT
}
EOF
    exit 1
  fi

  sleep "$RETRY_DELAY"
done
