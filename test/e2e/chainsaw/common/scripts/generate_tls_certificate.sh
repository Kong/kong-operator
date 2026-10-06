#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   HOSTNAME: The primary FQDN to use for the certificate CN.
#   SANS: (optional) Comma-separated list of additional SANs. If not provided, only HOSTNAME is used.
#   EXPIRED: (optional) When "true", generate a certificate whose validity period
#            ended in the past (2020-01-01 to 2020-01-02), to exercise expired
#            certificate handling.

HOSTNAME="${HOSTNAME}"
SANS="${SANS:-}"
EXPIRED="${EXPIRED:-false}"

tmp_dir=$(mktemp -d)
tmp_key="${tmp_dir}/tls.key"
tmp_crt="${tmp_dir}/tls.crt"
trap 'rm -rf "$tmp_dir"' EXIT

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

generate_valid() {
  openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout "$tmp_key" \
    -out "$tmp_crt" \
    -subj "/CN=${HOSTNAME}" \
    -addext "subjectAltName = ${SAN_LIST}"
}

# generate_expired self-signs a certificate with an explicit validity period in
# the past. `openssl req -x509` can't set past dates before OpenSSL 3.4, so this
# uses `openssl ca -selfsign`, which supports -startdate/-enddate on every
# OpenSSL version.
generate_expired() {
  touch "${tmp_dir}/index.txt"
  echo 01 > "${tmp_dir}/serial"
  printf '%s\n' \
    '[ca]' \
    'default_ca = CA_default' \
    '[CA_default]' \
    "dir = ${tmp_dir}" \
    "database = ${tmp_dir}/index.txt" \
    "serial = ${tmp_dir}/serial" \
    "new_certs_dir = ${tmp_dir}" \
    'default_md = sha256' \
    'policy = policy_any' \
    'copy_extensions = copy' \
    'unique_subject = no' \
    '[policy_any]' \
    'commonName = supplied' \
    > "${tmp_dir}/ca.cnf"
  openssl req -new -nodes -newkey rsa:2048 \
    -keyout "$tmp_key" \
    -out "${tmp_dir}/req.csr" \
    -subj "/CN=${HOSTNAME}" \
    -addext "subjectAltName = ${SAN_LIST}" &&
  openssl ca -batch -notext -selfsign \
    -config "${tmp_dir}/ca.cnf" \
    -keyfile "$tmp_key" \
    -in "${tmp_dir}/req.csr" \
    -out "$tmp_crt" \
    -startdate 20200101000000Z \
    -enddate 20200102000000Z
}

generate=generate_valid
if [[ "$EXPIRED" == "true" ]]; then
  generate=generate_expired
fi

# Redirect logs to stderr (>&2) so stdout stays pure JSON.
if ! OPENSSL_OUTPUT=$("$generate" 2>&1); then
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
