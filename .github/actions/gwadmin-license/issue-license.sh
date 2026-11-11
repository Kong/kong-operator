#!/usr/bin/env bash
# Inputs are supplied through the action's environment, never command arguments.
# issue requires explicit signing inputs, RUNNER_TEMP and GITHUB_OUTPUT.
# cleanup removes only this action's named private files, without signing.
set +x
set -euo pipefail
umask 077

fail() {
  echo "::error::GWAdmin license signing: $1" >&2
  exit 1
}

require_value() {
  [[ -n "$2" && ! "$2" =~ ^[[:space:]]*$ ]] || fail "missing required $1"
}

[[ "${RUNNER_TEMP:-}" = /* && -d "$RUNNER_TEMP" &&
   "$RUNNER_TEMP" != *$'\n'* && "$RUNNER_TEMP" != *$'\r'* ]] ||
  fail "RUNNER_TEMP must be an existing absolute runner-local directory"
license_dir="$RUNNER_TEMP/gwadmin-license"
license_file="$license_dir/license.jws"
[[ ! -L "$license_dir" ]] || fail "private license directory must not be a symbolic link"

cleanup() {
  if [[ -d "$license_dir" ]]; then
    rm -f "$license_dir/curl.conf" "$license_dir/request.json" "$license_dir/response"
    if [[ "${keep_license:-false}" != true ]]; then
      rm -f "$license_file"
      rmdir "$license_dir"
    fi
  fi
}

case "${1:-}" in
  cleanup)
    cleanup
    exit 0
    ;;
  issue) ;;
  *) fail "expected action 'issue' or 'cleanup'" ;;
esac

require_value "signing username" "${GW_ADMIN_LICENSE_SIGNING_USERNAME:-}"
require_value "signing password" "${GW_ADMIN_LICENSE_SIGNING_PASSWORD:-}"
require_value "customer" "${GW_ADMIN_LICENSE_CUSTOMER:-}"
require_value "license-key" "${GW_ADMIN_LICENSE_KEY:-}"
require_value "expiration-date" "${GW_ADMIN_LICENSE_EXPIRATION_DATE:-}"
[[ -f "${GITHUB_OUTPUT:-}" && -w "$GITHUB_OUTPUT" ]] ||
  fail "GITHUB_OUTPUT must name a writable action output file"

if ! jq -ne '
  env.GW_ADMIN_LICENSE_EXPIRATION_DATE as $day |
  ($day | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$")) and
  (($day | strptime("%Y-%m-%d") | mktime | strftime("%Y-%m-%d")) == $day) and
  ($day > (now | strftime("%Y-%m-%d")))
' >/dev/null 2>&1; then
  fail "expiration-date must be a future UTC date in YYYY-MM-DD format; no date or lifetime is inferred"
fi

mkdir -m 700 "$license_dir" ||
  fail "could not create a new private license directory; clean any previous action files first"
keep_license=false
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# Disable curl's default config and provide Basic auth only through a 0600 file.
# jq reads values from the environment, keeping credentials and license_key out
# of subprocess arguments and correctly escaping customer/key JSON strings.
if ! jq -nr '
  "header = " + ("Authorization: Basic " +
    ((env.GW_ADMIN_LICENSE_SIGNING_USERNAME + ":" + env.GW_ADMIN_LICENSE_SIGNING_PASSWORD) | @base64) | @json)
' >"$license_dir/curl.conf" 2>/dev/null ||
   ! jq -n '{
     customer: env.GW_ADMIN_LICENSE_CUSTOMER,
     license_expiration_date: env.GW_ADMIN_LICENSE_EXPIRATION_DATE,
     license_key: env.GW_ADMIN_LICENSE_KEY,
     entitlements: ["event_gateway"]
   }' >"$license_dir/request.json" 2>/dev/null; then
  fail "could not prepare the private signing request"
fi

if ! status="$(curl --disable --silent --show-error --proto '=https' \
    --connect-timeout 10 --max-time 60 \
    --config "$license_dir/curl.conf" \
    --request POST --header 'Content-Type: application/json' \
    --data-binary "@$license_dir/request.json" \
    --output "$license_dir/response" --write-out '%{http_code}' \
    https://license.api.konghq.tech/v1/license/enveloped 2>/dev/null)"; then
  fail "signing service transport failed (payload-bearing curl diagnostics suppressed)"
fi
[[ "$status" =~ ^[0-9]{3}$ ]] || fail "signing service returned an invalid HTTP status"
[[ "$status" =~ ^2[0-9]{2}$ ]] || fail "signing service rejected the request (HTTP $status; response body suppressed)"

# A successful response must supply the original compact JWS. Signature,
# expiration and entitlement validation remain gw-admin's responsibility.
cp "$license_dir/response" "$license_file" ||
  fail "could not prepare the private license file"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! GW_ADMIN_LICENSE_FILE="$license_file" \
    bash "$SCRIPT_DIR/../../../test/e2e/chainsaw/fixtures/gwadmin-license.sh" check >/dev/null 2>&1; then
  fail "signing service did not return a compact JWS enveloped license (response body suppressed)"
fi
printf 'license-file=%s\n' "$license_file" >>"$GITHUB_OUTPUT"
keep_license=true
echo "GWAdmin enveloped license input acquired; signature/expiry/entitlements are validated by gw-admin"
