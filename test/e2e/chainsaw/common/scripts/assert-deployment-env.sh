#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Asserts the environment of a container in a Deployment: waits for the
# Deployment to exist, then checks that every pair in EXPECTED_ENV is set to
# the expected value. Exits non-zero on the first mismatch, naming it.
#
# Variables (from environment):
#   NAMESPACE: The namespace of the Deployment.
#   DEPLOYMENT_LABEL_SELECTOR: Label selector matching exactly one Deployment
#     (names are often generated, so the selector is what identifies it).
#   CONTAINER: The container whose environment is checked.
#   EXPECTED_ENV: Semicolon-separated NAME=value pairs, e.g. "A=1;B=2".
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '60'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '2'.

: "${NAMESPACE:?NAMESPACE must be set}"
: "${DEPLOYMENT_LABEL_SELECTOR:?DEPLOYMENT_LABEL_SELECTOR must be set}"
: "${CONTAINER:?CONTAINER must be set}"
: "${EXPECTED_ENV:?EXPECTED_ENV must be set}"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_DELAY="${RETRY_DELAY:-2}"

deployment_json=""
attempt=0
until deployment_json="$(kubectl -n "${NAMESPACE}" get deploy -l "${DEPLOYMENT_LABEL_SELECTOR}" -o json 2>/dev/null)" &&
  [ "$(jq -r '.items | length' <<<"${deployment_json}")" -gt 0 ]; do
  attempt=$((attempt + 1))
  if (( attempt >= MAX_RETRIES )); then
    echo "no Deployment matching ${DEPLOYMENT_LABEL_SELECTOR} in ${NAMESPACE} after ${MAX_RETRIES} attempts" >&2
    exit 1
  fi
  sleep "${RETRY_DELAY}"
done

container_env="$(jq -r --arg c "${CONTAINER}" \
  '.items[0].spec.template.spec.containers[] | select(.name == $c) | .env // []' <<<"${deployment_json}")"
if [ "${container_env}" = "[]" ] && ! jq -e --arg c "${CONTAINER}" \
  '.items[0].spec.template.spec.containers[] | select(.name == $c)' <<<"${deployment_json}" >/dev/null; then
  echo "no container ${CONTAINER} in the Deployment matching ${DEPLOYMENT_LABEL_SELECTOR}" >&2
  exit 1
fi

while IFS= read -r pair; do
  [ -n "${pair}" ] || continue
  name="${pair%%=*}"
  expected="${pair#*=}"
  got="$(jq -r --arg c "${CONTAINER}" --arg n "${name}" \
    '.items[0].spec.template.spec.containers[] | select(.name == $c) | (.env // [])[] | select(.name == $n) | .value // ""' \
    <<<"${deployment_json}")"
  if [ "${got}" != "${expected}" ]; then
    echo "${name} is '${got}', expected '${expected}'" >&2
    exit 1
  fi
  echo "ok: ${name}=${got}"
done < <(tr ';' '\n' <<<"${EXPECTED_ENV}")
