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
#   RETRIES: (optional) Number of attempts to match fingerprints. Default: 12.
#   WAIT: (optional) Seconds to wait between attempts. Default: 10.


PROXY_IP="${PROXY_IP}"
SNI_HOSTNAME="${SNI_HOSTNAME}"
SECRET_NAME="${SECRET_NAME}"
SECRET_NAMESPACE="${SECRET_NAMESPACE}"

# Optional variables (Retries and Wait) can keep their defaults.
RETRIES="${RETRIES:-12}"
WAIT="${WAIT:-10}"

# Temporary file cleanup
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Function for structured output on logical completion (success or failure)
output_json() {
        local status="$1"
        local message="$2"
        local server_fp="$3"
        local secret_fp="$4"
        cat <<EOF
{
    "status": "$status",
    "message": "$message",
    "server_fingerprint": "$server_fp",
    "secret_fingerprint": "$secret_fp",
    "sni_hostname": "$SNI_HOSTNAME",
    "proxy_ip": "$PROXY_IP",
    "secret_name": "$SECRET_NAME",
    "secret_namespace": "$SECRET_NAMESPACE"
}
EOF
}

# 2. Extract Secret fingerprint
# If kubectl fails (e.g. secret not found), errexit will kill the script immediately.
SECRET_DATA=$(kubectl get secret "$SECRET_NAME" -n "$SECRET_NAMESPACE" -o jsonpath="{.data.tls\\.crt}")
echo "$SECRET_DATA" | base64 --decode > "$TMP_DIR/secret.crt"
SECRET_FP=$(openssl x509 -in "$TMP_DIR/secret.crt" -noout -fingerprint 2>/dev/null)

# 3. Retry loop to compare fingerprints
SERVER_FP="none"
OPENSSL_OUTPUT=""
for i in $(seq 1 "$RETRIES"); do
    # Attempt to capture the server certificate via openssl
    OPENSSL_OUTPUT=$(timeout 5s openssl s_client -connect "${PROXY_IP}:443" -servername "${SNI_HOSTNAME}" </dev/null -showcerts 2>&1 || true)
    
    if echo "$OPENSSL_OUTPUT" | awk '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/' > "$TMP_DIR/server.crt" 2>/dev/null && [ -s "$TMP_DIR/server.crt" ]; then
        SERVER_FP=$(openssl x509 -in "$TMP_DIR/server.crt" -noout -fingerprint 2>/dev/null || echo "invalid")
        
        if [ "$SERVER_FP" = "$SECRET_FP" ]; then
            output_json "success" "Fingerprints match" "$SERVER_FP" "$SECRET_FP"
            exit 0
        fi
    fi

    [ "$i" -lt "$RETRIES" ] && sleep "$WAIT"
done

# 4. Logical Failure (Variables were set, but fingerprints didn't match after retries)
cat <<EOF
{
    "status": "failure",
    "message": "Fingerprints did not match after $RETRIES attempts",
    "server_fingerprint": "$SERVER_FP",
    "secret_fingerprint": "$SECRET_FP",
    "sni_hostname": "$SNI_HOSTNAME",
    "proxy_ip": "$PROXY_IP",
    "secret_name": "$SECRET_NAME",
    "secret_namespace": "$SECRET_NAMESPACE",
    "openssl_output": $(echo "$OPENSSL_OUTPUT" | jq -Rs .)
}
EOF
exit 1