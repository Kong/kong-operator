#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   HOSTNAME: The primary FQDN to use for the certificate CN.
#   SANS: (optional) Comma-separated list of additional SANs. If not provided, only HOSTNAME is used.

HOSTNAME="${HOSTNAME}"
SANS="${SANS:-}"

tmp_key=$(mktemp)
tmp_crt=$(mktemp)
trap 'rm -f "$tmp_key" "$tmp_crt"' EXIT

# Build the SAN list
if [[ -n "$SANS" ]]; then
  # Split SANS by comma and build the subjectAltName string
  SAN_LIST="DNS:${HOSTNAME}"
  IFS=',' read -ra SAN_ARRAY <<< "$SANS"
  for SAN in "${SAN_ARRAY[@]}"; do
    SAN=$(echo "$SAN" | xargs)  # trim whitespace
    SAN_LIST="${SAN_LIST},DNS:${SAN}"
  done
else
  SAN_LIST="DNS:${HOSTNAME}"
fi

# Redirect logs to stderr (>&2) so stdout stays pure JSON.
if ! OPENSSL_OUTPUT=$(openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout "$tmp_key" \
  -out "$tmp_crt" \
  -subj "/CN=${HOSTNAME}" \
  -addext "subjectAltName = ${SAN_LIST}" 2>&1); then
  jq -n \
    --arg error "OpenSSL certificate generation failed" \
    --arg hostname "$HOSTNAME" \
    --arg sans "$SANS" \
    --arg openssl_output "$OPENSSL_OUTPUT" \
    '{error: $error, hostname: $hostname, sans: $sans, openssl_output: $openssl_output}'
  exit 1
fi

jq -n \
  --arg cert "$(cat "$tmp_crt")" \
  --arg key "$(cat "$tmp_key")" \
  '{cert: $cert, key: $key}'
