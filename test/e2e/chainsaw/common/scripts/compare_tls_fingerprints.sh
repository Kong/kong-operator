#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   SNI_HOSTNAME: The SNI hostname to use for the TLS handshake.
#   SECRET_NAME: The name of the Kubernetes TLS secret.
#   SECRET_NAMESPACE: The namespace of the Kubernetes TLS secret.
#   MAX_RETRIES: (optional) Number of attempts to match fingerprints. Default: 180.
#   RETRY_DELAY: (optional) Seconds to wait between attempts. Default: 1.

PROXY_IP="${PROXY_IP}"
SNI_HOSTNAME="${SNI_HOSTNAME}"
SECRET_NAME="${SECRET_NAME}"
SECRET_NAMESPACE="${SECRET_NAMESPACE}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Temporary file cleanup.
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Extract Secret fingerprint.
# If kubectl fails (e.g. secret not found), errexit will kill the script immediately.
SECRET_DATA=$(kubectl get secret "$SECRET_NAME" -n "$SECRET_NAMESPACE" -o jsonpath="{.data.tls\\.crt}")
echo "$SECRET_DATA" | base64 --decode > "$TMP_DIR/secret.crt"
SECRET_FP=$(openssl x509 -in "$TMP_DIR/secret.crt" -noout -fingerprint 2>/dev/null)

# Build openssl command.
OPENSSL_CMD="openssl s_client -connect ${PROXY_IP}:443 -servername ${SNI_HOSTNAME}"

# Retry loop to compare fingerprints.
SERVER_FP="none"
OPENSSL_OUTPUT=""

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  # Attempt to capture the server certificate via openssl.
  OPENSSL_OUTPUT=$($OPENSSL_CMD </dev/null -showcerts 2>&1 || true)

  if echo "$OPENSSL_OUTPUT" | awk '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/' > "$TMP_DIR/server.crt" 2>/dev/null && [ -s "$TMP_DIR/server.crt" ]; then
    SERVER_FP=$(openssl x509 -in "$TMP_DIR/server.crt" -noout -fingerprint 2>/dev/null || echo "invalid")

    if [ "$SERVER_FP" = "$SECRET_FP" ]; then
      cat <<EOF
{
  "status": "success",
  "message": "Fingerprints match",
  "server_fingerprint": "$SERVER_FP",
  "secret_fingerprint": "$SECRET_FP",
  "sni_hostname": "$SNI_HOSTNAME",
  "proxy_ip": "$PROXY_IP",
  "secret_name": "$SECRET_NAME",
  "secret_namespace": "$SECRET_NAMESPACE",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "openssl_command": "$OPENSSL_CMD"
}
EOF
      exit 0
    fi
  fi

  [ "$ATTEMPT" -lt "$MAX_RETRIES" ] && sleep "$RETRY_DELAY"
done

# Logical Failure (Variables were set, but fingerprints didn't match after retries).
cat <<EOF
{
  "status": "failure",
  "message": "Fingerprints did not match after $MAX_RETRIES attempts",
  "server_fingerprint": "$SERVER_FP",
  "secret_fingerprint": "$SECRET_FP",
  "sni_hostname": "$SNI_HOSTNAME",
  "proxy_ip": "$PROXY_IP",
  "secret_name": "$SECRET_NAME",
  "secret_namespace": "$SECRET_NAMESPACE",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "openssl_command": "$OPENSSL_CMD",
  "openssl_output": $(echo "$OPENSSL_OUTPUT" | jq -Rs .)
}
EOF
exit 1
