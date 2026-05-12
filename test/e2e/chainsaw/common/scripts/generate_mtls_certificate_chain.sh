#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   CA_COMMON_NAME: The CN to use for the self-signed CA certificate.
#   SERVER_COMMON_NAME: The primary CN/SAN to use for the server certificate.
#   SERVER_SANS: (optional) Comma-separated list of additional DNS SANs for the server cert.
#   CLIENT_COMMON_NAME: The CN to use for the client certificate.
#
# Output (JSON on stdout):
#   {
#     "ca_cert":     "<PEM>",
#     "ca_key":      "<PEM>",
#     "server_cert": "<PEM>",
#     "server_key":  "<PEM>",
#     "client_cert": "<PEM>",
#     "client_key":  "<PEM>"
#   }

CA_COMMON_NAME="${CA_COMMON_NAME}"
SERVER_COMMON_NAME="${SERVER_COMMON_NAME}"
SERVER_SANS="${SERVER_SANS:-}"
CLIENT_COMMON_NAME="${CLIENT_COMMON_NAME}"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

ca_key="${tmpdir}/ca.key"
ca_crt="${tmpdir}/ca.crt"
ca_srl="${tmpdir}/ca.srl"

server_key="${tmpdir}/server.key"
server_csr="${tmpdir}/server.csr"
server_crt="${tmpdir}/server.crt"
server_ext="${tmpdir}/server.ext"

client_key="${tmpdir}/client.key"
client_csr="${tmpdir}/client.csr"
client_crt="${tmpdir}/client.crt"
client_ext="${tmpdir}/client.ext"

ca_ext="${tmpdir}/ca.ext"

emit_error() {
  local stage="$1"
  local output="$2"
  jq -n \
    --arg error "OpenSSL ${stage} failed" \
    --arg ca_common_name "$CA_COMMON_NAME" \
    --arg server_common_name "$SERVER_COMMON_NAME" \
    --arg server_sans "$SERVER_SANS" \
    --arg client_common_name "$CLIENT_COMMON_NAME" \
    --arg openssl_output "$output" \
    '{error: $error, ca_common_name: $ca_common_name, server_common_name: $server_common_name, server_sans: $server_sans, client_common_name: $client_common_name, openssl_output: $openssl_output}'
}

# Build the SAN list for the server cert (always include SERVER_COMMON_NAME as DNS:0).
SAN_LIST="DNS:${SERVER_COMMON_NAME}"
if [[ -n "$SERVER_SANS" ]]; then
  IFS=',' read -ra SAN_ARRAY <<< "$SERVER_SANS"
  for SAN in "${SAN_ARRAY[@]}"; do
    SAN=$(echo "$SAN" | xargs)  # trim whitespace
    if [[ -n "$SAN" ]]; then
      SAN_LIST="${SAN_LIST},DNS:${SAN}"
    fi
  done
fi

# --- 1. Generate self-signed CA ---
cat > "$ca_ext" <<EOF
basicConstraints = critical, CA:TRUE
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
EOF

if ! OPENSSL_OUTPUT=$(openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
  -keyout "$ca_key" \
  -out "$ca_crt" \
  -subj "/CN=${CA_COMMON_NAME}" \
  -extensions v3_ca \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" 2>&1); then
  emit_error "CA generation" "$OPENSSL_OUTPUT"
  exit 1
fi

# --- 2. Generate server key + CSR ---
if ! OPENSSL_OUTPUT=$(openssl req -new -newkey rsa:2048 -nodes \
  -keyout "$server_key" \
  -out "$server_csr" \
  -subj "/CN=${SERVER_COMMON_NAME}" 2>&1); then
  emit_error "server CSR generation" "$OPENSSL_OUTPUT"
  exit 1
fi

# --- 3. Sign server cert with CA ---
cat > "$server_ext" <<EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = ${SAN_LIST}
EOF

if ! OPENSSL_OUTPUT=$(openssl x509 -req -days 365 \
  -in "$server_csr" \
  -CA "$ca_crt" \
  -CAkey "$ca_key" \
  -CAcreateserial \
  -CAserial "$ca_srl" \
  -out "$server_crt" \
  -extfile "$server_ext" 2>&1); then
  emit_error "server cert signing" "$OPENSSL_OUTPUT"
  exit 1
fi

# --- 4. Generate client key + CSR ---
if ! OPENSSL_OUTPUT=$(openssl req -new -newkey rsa:2048 -nodes \
  -keyout "$client_key" \
  -out "$client_csr" \
  -subj "/CN=${CLIENT_COMMON_NAME}" 2>&1); then
  emit_error "client CSR generation" "$OPENSSL_OUTPUT"
  exit 1
fi

# --- 5. Sign client cert with CA ---
cat > "$client_ext" <<EOF
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

if ! OPENSSL_OUTPUT=$(openssl x509 -req -days 365 \
  -in "$client_csr" \
  -CA "$ca_crt" \
  -CAkey "$ca_key" \
  -CAserial "$ca_srl" \
  -out "$client_crt" \
  -extfile "$client_ext" 2>&1); then
  emit_error "client cert signing" "$OPENSSL_OUTPUT"
  exit 1
fi

# --- 6. Emit JSON ---
jq -n \
  --arg ca_cert "$(cat "$ca_crt")" \
  --arg ca_key "$(cat "$ca_key")" \
  --arg server_cert "$(cat "$server_crt")" \
  --arg server_key "$(cat "$server_key")" \
  --arg client_cert "$(cat "$client_crt")" \
  --arg client_key "$(cat "$client_key")" \
  '{ca_cert: $ca_cert, ca_key: $ca_key, server_cert: $server_cert, server_key: $server_key, client_cert: $client_cert, client_key: $client_key}'
