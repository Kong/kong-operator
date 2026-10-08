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
#   NOT_YET_VALID: (optional) When "true", generate a certificate whose validity
#            period starts in the future (2099-01-01 to 2099-12-31), to exercise
#            not-yet-valid certificate handling.

HOSTNAME="${HOSTNAME}"
SANS="${SANS:-}"
EXPIRED="${EXPIRED:-false}"
NOT_YET_VALID="${NOT_YET_VALID:-false}"

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

# generate_with_dates self-signs a certificate valid from $1 to $2 (YYYYMMDDHHMMSSZ).
# `openssl req -x509` can't set explicit dates before OpenSSL 3.4, so this uses
# `openssl ca -selfsign`, which supports -startdate/-enddate on every OpenSSL
# version.
generate_with_dates() {
  local start_date="$1" end_date="$2"
  # errexit is disabled inside the $(...) this runs in, so every step returns
  # explicitly on failure. The certificate's extensions come from an explicit
  # [v3_ext] section (rather than copy_extensions), which makes LibreSSL issue
  # an X.509 v3 certificate too.
  touch "${tmp_dir}/index.txt" || return 1
  echo 01 > "${tmp_dir}/serial" || return 1
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
    'unique_subject = no' \
    '[policy_any]' \
    'commonName = supplied' \
    '[v3_ext]' \
    'basicConstraints = CA:FALSE' \
    "subjectAltName = ${SAN_LIST}" \
    > "${tmp_dir}/ca.cnf" || return 1
  openssl req -new -nodes -newkey rsa:2048 \
    -keyout "$tmp_key" \
    -out "${tmp_dir}/req.csr" \
    -subj "/CN=${HOSTNAME}" || return 1
  openssl ca -batch -notext -selfsign \
    -config "${tmp_dir}/ca.cnf" \
    -extensions v3_ext \
    -keyfile "$tmp_key" \
    -in "${tmp_dir}/req.csr" \
    -out "$tmp_crt" \
    -startdate "$start_date" \
    -enddate "$end_date"
}

generate_expired() {
  generate_with_dates 20200101000000Z 20200102000000Z
}

generate_not_yet_valid() {
  generate_with_dates 20990101000000Z 20991231000000Z
}

generate=generate_valid
if [[ "$EXPIRED" == "true" ]]; then
  generate=generate_expired
elif [[ "$NOT_YET_VALID" == "true" ]]; then
  generate=generate_not_yet_valid
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
