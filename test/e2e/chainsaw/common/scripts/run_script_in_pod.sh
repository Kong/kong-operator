#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Runs another script from inside the cluster (e.g. to reach a ClusterIP
# Service or a cluster-DNS name), by piping it into a short-lived curl Pod's
# stdin.
#
# The environment variables the target script reads are forwarded to the Pod
# automatically: every top-level 'VAR="${INPUT...}"' assignment in the target
# script declares INPUT as one of its inputs, and each such input that is
# exported to this script is passed through with --env. Unset inputs are left
# to the target script's own defaults. Never forwarded, even when exported:
# this script's own variables and host-specific ones (see DENIED_VARS).
#
# Values are forwarded verbatim, so inputs that refer to files on the host
# running chainsaw (e.g. CACERT_PATH) do not work in the Pod: leave them unset.
#
# Usage:
#   bash run_script_in_pod.sh <script path>
#
# Variables (from environment):
#   POD_NAMESPACE: (optional) Namespace for the test Pod. Default: $NAMESPACE, or 'default'.
#   CURL_IMAGE: (optional) Image for the test Pod. Default: curlimages/curl:latest.
#   Any input of the target script (see above).

# Snapshot the exported variables before this script defines its own.
EXPORTED_VARS=" $(compgen -e | tr '\n' ' ') "
DENIED_VARS=" SCRIPT_PATH POD_NAMESPACE POD_NAME CURL_IMAGE HOSTNAME HOME PATH PWD SHELL TMPDIR USER "

SCRIPT_PATH="${1:?usage: run_script_in_pod.sh <script path>}"
POD_NAMESPACE="${POD_NAMESPACE:-${NAMESPACE:-default}}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:latest}"
POD_NAME="chainsaw-in-cluster-$(date +%s)-${RANDOM}"

ENV_ARGS=()
for VAR in $(sed -nE 's/^[A-Z_][A-Z0-9_]*="\$\{([A-Z_][A-Z0-9_]*).*/\1/p' "${SCRIPT_PATH}" | sort -u); do
  if [[ "${EXPORTED_VARS}" == *" ${VAR} "* && "${DENIED_VARS}" != *" ${VAR} "* ]]; then
    ENV_ARGS+=(--env="${VAR}=${!VAR}")
  fi
done

kubectl run "${POD_NAME}" \
  --image="${CURL_IMAGE}" --image-pull-policy=IfNotPresent \
  --rm -i --restart=Never \
  ${ENV_ARGS[@]+"${ENV_ARGS[@]}"} \
  -n "${POD_NAMESPACE}" \
  -- sh -s <"${SCRIPT_PATH}" 2>/dev/null | grep -v '^pod "'
