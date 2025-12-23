# abort on nonzero exitstatus, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Check for HOSTNAME
if [ -z "${HOSTNAME:-}" ]; then 
  echo "Error: HOSTNAME environment variable is not set." >&2; 
  exit 1
fi

tmp_key=$(mktemp)
tmp_crt=$(mktemp)
trap 'rm -f "$tmp_key" "$tmp_crt"' EXIT

# Redirect logs to stderr (>&2) so stdout stays pure JSON.
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout "$tmp_key" \
  -out "$tmp_crt" \
  -subj "/CN=${HOSTNAME}" \
  -addext "subjectAltName = DNS:${HOSTNAME}" >&2

jq -n \
  --arg cert "$(cat "$tmp_crt")" \
  --arg key "$(cat "$tmp_key")" \
  '{cert: $cert, key: $key}'