#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Generates a 2048-bit RSA key pair and outputs JSON:
#   {"private_key": "<PKCS8 PEM>", "public_key": "<SPKI PEM>"}
# Stdout is pure JSON; progress/error output goes to stderr.

tmp_key=$(mktemp)
tmp_pub=$(mktemp)
trap 'rm -f "$tmp_key" "$tmp_pub"' EXIT

# Generate RSA private key in PKCS#8 format ("BEGIN PRIVATE KEY").
if ! OPENSSL_OUTPUT=$(openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$tmp_key" 2>&1); then
  jq -n \
    --arg error "RSA key generation failed" \
    --arg openssl_output "$OPENSSL_OUTPUT" \
    '{error: $error, openssl_output: $openssl_output}'
  exit 1
fi

# Extract the public key in SPKI format ("BEGIN PUBLIC KEY").
if ! OPENSSL_OUTPUT=$(openssl pkey -in "$tmp_key" -pubout -out "$tmp_pub" 2>&1); then
  jq -n \
    --arg error "RSA public key extraction failed" \
    --arg openssl_output "$OPENSSL_OUTPUT" \
    '{error: $error, openssl_output: $openssl_output}'
  exit 1
fi

jq -n \
  --arg private_key "$(cat "$tmp_key")" \
  --arg public_key "$(cat "$tmp_pub")" \
  '{private_key: $private_key, public_key: $public_key}'
