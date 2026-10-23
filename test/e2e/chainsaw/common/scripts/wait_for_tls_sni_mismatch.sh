#!/usr/bin/env bash
# Poll host_connectivity_test_with_hostname_tls_check.sh until it reports
# certificate_match != true, or an outer budget is exhausted. For observing
# Konnect -> AI Gateway DataPlane config propagation (e.g. after deleting an
# AIGatewaySNI): there's no fixed completion signal to wait on other than
# polling live traffic, and host_connectivity_test_with_hostname_tls_check.sh's
# own MAX_RETRIES loop only waits for a match to APPEAR, not to disappear -
# the opposite condition. Chainsaw doesn't re-poll a script step's `check:`
# either (see test/e2e/chainsaw/README.md), so the wait has to happen here.
#
# Required env: same as host_connectivity_test_with_hostname_tls_check.sh
# (FQDN, PROXY_IP, METHOD, ROUTE_PATH, INSECURE, EXPECTED_STATUS_REGEX), plus:
#   NAMESPACE, CA_SECRET_NAME   Passed through to write_ca_cert_file.sh.
# Optional env:
#   POLL_ATTEMPTS   Outer poll attempts. Default: 40.
#   POLL_DELAY      Seconds between outer polls. Default: 3.
#   CONFIRMATIONS   Number of *consecutive* non-matching attempts required
#                   before concluding the mapping is gone. Default: 3. The
#                   inner script reports certificate_match=false for ANY curl
#                   failure (connection refused, TLS blip), not only for a
#                   genuine mismatch, so a lone transient failure must not
#                   conclude the poll - symmetric with REJECT_CONFIRMATIONS
#                   in aigw_chat_completion.sh.
#
# Chainsaw usage: json_parse($stdout).certificate_match
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
POLL_ATTEMPTS="${POLL_ATTEMPTS:-40}"
POLL_DELAY="${POLL_DELAY:-3}"
CONFIRMATIONS="${CONFIRMATIONS:-3}"

CA_CERT_RESULT=$(bash "${SCRIPT_DIR}/write_ca_cert_file.sh")
CACERT_PATH=$(echo "${CA_CERT_RESULT}" | jq -r '.ca_cert_path')
export CACERT_PATH
trap 'rm -f "${CACERT_PATH}"' EXIT

# Each call gets exactly one attempt: this script owns the "keep trying until
# the match disappears" loop, not the inner script's own retry logic.
export MAX_RETRIES=1

LAST_OUTPUT=""
CONSECUTIVE=0
for ATTEMPT in $(seq 1 "${POLL_ATTEMPTS}"); do
  LAST_OUTPUT=$(bash "${SCRIPT_DIR}/host_connectivity_test_with_hostname_tls_check.sh" || true)
  # tostring keeps a genuine JSON false distinct from a missing key: jq's
  # "//" operator filters out boolean false, so ".certificate_match // empty"
  # would hide the very mismatch this script waits for.
  MATCH=$(echo "${LAST_OUTPUT}" | jq -r 'if type == "object" and has("certificate_match") then (.certificate_match | tostring) else empty end') || MATCH=""
  if [ -z "${MATCH}" ]; then
    # The inner script itself broke (no parseable result): that is an
    # infrastructure error, not evidence of a mismatch - fail loudly instead
    # of counting it toward the confirmation.
    echo "wait_for_tls_sni_mismatch: inner script produced no result (attempt ${ATTEMPT})" >&2
    exit 1
  fi
  if [[ "${MATCH}" != "true" ]]; then
    CONSECUTIVE=$((CONSECUTIVE + 1))
    if [[ "${CONSECUTIVE}" -ge "${CONFIRMATIONS}" ]]; then
      break
    fi
  else
    CONSECUTIVE=0
  fi
  sleep "${POLL_DELAY}"
done

echo "${LAST_OUTPUT}"
