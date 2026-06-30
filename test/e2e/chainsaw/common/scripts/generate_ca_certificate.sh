#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Generates a self-signed CA certificate and outputs JSON: {cert: "<pem>"}.
# The certificate has basicConstraints=critical,CA:TRUE so it is accepted as a CA cert.

tmp_key=$(mktemp)
tmp_crt=$(mktemp)
trap 'rm -f "$tmp_key" "$tmp_crt"' EXIT

if ! OPENSSL_OUTPUT=$(openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
  -keyout "$tmp_key" \
  -out "$tmp_crt" \
  -subj "/CN=test-ca" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" 2>&1); then
  jq -n \
    --arg error "OpenSSL CA certificate generation failed" \
    --arg openssl_output "$OPENSSL_OUTPUT" \
    '{error: $error, openssl_output: $openssl_output}'
  exit 1
fi

jq -n \
  --arg cert "$(cat "$tmp_crt")" \
  '{cert: $cert}'
