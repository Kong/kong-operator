#!/bin/sh
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   FQDN: The fully qualified domain name to test.
#   PROXY_IP: The IP address of the proxy to connect to.
#   METHOD: The HTTP method to use (e.g., 'GET', 'POST', 'PUT').
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.
#   INSECURE: (optional) If 'true', disables TLS verification. Default: 'true'.
#             Ignored when CACERT_PATH is set (see below).
#   CACERT_PATH: (optional) Path to a local CA certificate file. When set to a
#             non-empty value, the request validates the server's certificate
#             chain against this CA (curl --cacert) instead of skipping TLS
#             verification, so real chain-of-trust validation can be proven
#             (not just cert-hostname matching with verification disabled).
#             Takes precedence over INSECURE. Default: unset (falls back to
#             the INSECURE behavior described above, unchanged).
#   EXPECTED_STATUS_REGEX: (optional) Extended regex the HTTP status
#             code must match for the request to count as successful (the
#             certificate still has to match on top of this). Default:
#             '^200$', unchanged from before this option existed. Use e.g.
#             '^[0-9]{3}$' to accept any well-formed HTTP response when only
#             the TLS/SNI/certificate handshake is under test, independent of
#             whether the request is also routed successfully at the
#             application layer.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

FQDN="${FQDN}"
PROXY_IP="${PROXY_IP}"
# Bracket PROXY_IP for use in a host:port string when it's an IPv6 address
# (identified by containing a colon), matching RFC 3986.
case "$PROXY_IP" in
  *:*) PROXY_HOST="[${PROXY_IP}]" ;;
  *) PROXY_HOST="$PROXY_IP" ;;
esac
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
INSECURE="${INSECURE:-true}"
CACERT_PATH="${CACERT_PATH:-}"
EXPECTED_STATUS_REGEX="${EXPECTED_STATUS_REGEX:-^200$}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Determine the TLS verification flag. CACERT_PATH (real chain-of-trust
# validation against a specific CA) takes precedence over INSECURE (skip
# verification entirely). When CACERT_PATH is unset/empty, behavior is
# unchanged from before this option existed.
INSECURE_FLAG=""
if [ -n "$CACERT_PATH" ]; then
  INSECURE_FLAG="--cacert '${CACERT_PATH}'"
elif [ "$INSECURE" = "true" ]; then
  INSECURE_FLAG="--insecure"
fi

# Body temp file, cleaned up on any exit path.
BODY_FILE=$(mktemp /tmp/curl_body.XXXXXX)
trap 'rm -f "$BODY_FILE"' EXIT

CURL_CMD="curl -s -w '%{http_code}' -X $METHOD --resolve '${FQDN}:443:${PROXY_HOST}' 'https://${FQDN}${ROUTE_PATH}' -vv $INSECURE_FLAG -o $BODY_FILE"

# Pure shell JSON string escaping, since jq isn't available in the
# curlimages/curl image this script runs under for in-cluster checks.
json_escape() {
  printf '%s' "$1" | awk '
    {
      line = $0
      out = ""
      n = length(line)
      for (i = 1; i <= n; i++) {
        c = substr(line, i, 1)
        if (c == "\\") out = out "\\\\"
        else if (c == "\"") out = out "\\\""
        else out = out c
      }
      printf "%s%s", (NR > 1 ? "\\n" : ""), out
    }
  '
}

# Build a JSON object from "type:key=value" tokens (type is str/num/bool) and
# print it to stdout. One implementation shared by every exit path below.
emit_result() {
  local first=true kv type rest key value
  printf '{\n'
  for kv in "$@"; do
    type="${kv%%:*}"
    rest="${kv#*:}"
    key="${rest%%=*}"
    value="${rest#*=}"
    if [ "$first" = true ]; then
      first=false
    else
      printf ',\n'
    fi
    printf '  "%s": ' "$key"
    case "$type" in
      str) printf '"%s"' "$(json_escape "$value")" ;;
      num | bool) printf '%s' "$value" ;;
      *) echo "emit_result: unknown type '$type' for key '$key'" >&2; exit 1 ;;
    esac
  done
  printf '\n}\n'
}

# Sets POD_NODE, POD_NAME, POD_NAMESPACE, POD_IP from a response body of the
# form:
#   Welcome, you are connected to node <node>.
#   Running on Pod <pod-name>.
#   In namespace <namespace>.
#   With IP address <ip>.
extract_body_info() {
  local body="$1"
  POD_NODE=$(echo "$body" | grep -o 'connected to node [^.]*' | sed 's/connected to node //' || echo "")
  POD_NAME=$(echo "$body" | grep -o 'Running on Pod [^.]*' | sed 's/Running on Pod //' || echo "")
  POD_NAMESPACE=$(echo "$body" | grep -o 'In namespace [^.]*' | sed 's/In namespace //' || echo "")
  POD_IP=$(echo "$body" | grep -o 'IP address .*' | sed 's/IP address //' || echo "")
}

# Sets CERTIFICATE_MATCH ("true"/"false") and MESSAGE from comparing the
# presented certificate's hostname against $FQDN. Supports:
# 1. Exact match (echo.kong.example.com == echo.kong.example.com)
# 2. Wildcard match (*.kong.example.com covers echo.kong.example.com)
# 3. Parent domain match (kong.example.com covers echo.kong.example.com)
validate_certificate() {
  local actual_hostname="$1" wildcard_base

  if [ "$actual_hostname" = "$FQDN" ]; then
    CERTIFICATE_MATCH="true"
    MESSAGE="Certificate hostname validation passed: exact match $actual_hostname"
  elif printf '%s' "$actual_hostname" | grep -Eq '^\*\..+'; then
    wildcard_base="${actual_hostname#\*.}"
    case "$FQDN" in
      *."$wildcard_base" | "$wildcard_base")
        CERTIFICATE_MATCH="true"
        MESSAGE="Certificate hostname validation passed: wildcard $actual_hostname covers $FQDN"
        ;;
      *)
        CERTIFICATE_MATCH="false"
        MESSAGE="Certificate hostname mismatch: wildcard $actual_hostname does not cover $FQDN"
        ;;
    esac
  else
    case "$FQDN" in
      *."$actual_hostname")
        CERTIFICATE_MATCH="true"
        MESSAGE="Certificate hostname validation passed: parent domain $actual_hostname covers $FQDN"
        ;;
      *)
        CERTIFICATE_MATCH="false"
        MESSAGE="Certificate hostname mismatch: expected $FQDN or a subdomain of $actual_hostname, got $actual_hostname"
        ;;
    esac
  fi
}

# Extracts the hostname the server's certificate was actually matched/issued
# for out of curl's -vv output. Prefers curl's own hostname-match report:
# whenever curl actually verifies the hostname (i.e. TLS verification isn't
# disabled), it prints `subjectAltName: host "<fqdn>" matched cert's
# "<pattern>"`, where <pattern> is the exact SAN entry that matched, even a
# wildcard SAN (e.g. "*.example.com") if that's what matched, even when the
# certificate's own Subject CN is a different, more specific name (e.g.
# CN=api.example.com with SAN=*.example.com). Falls back to the Subject CN
# only when that line is absent (TLS verification was skipped, INSECURE=true).
resolve_actual_hostname() {
  local output="$1" hostname
  hostname=$(echo "$output" | grep -o 'subjectAltName: host "[^"]*" matched cert'"'"'s "[^"]*"' | sed -E 's/.*matched cert'"'"'s "([^"]*)"/\1/' || echo "")
  if [ -z "$hostname" ]; then
    hostname=$(echo "$output" | grep -o 'subject:.*CN=[^;]*' | sed 's/.*CN=//' | tr -d ' ' || echo "")
  fi
  echo "$hostname"
}

# Retry loop: Keep trying until the status matches EXPECTED_STATUS_REGEX and
# the certificate matches, or we run out of retries.
LAST_OUTPUT=""
LAST_HTTP_CODE=""
LAST_ACTUAL_HOSTNAME=""
LAST_MESSAGE=""
POD_NODE=""
POD_NAME=""
POD_NAMESPACE=""
POD_IP=""

for ATTEMPT in $(seq 1 "$MAX_RETRIES"); do
  > "$BODY_FILE"

  if OUTPUT=$(eval "$CURL_CMD" 2>&1); then
    LAST_OUTPUT="$OUTPUT"

    # The last line of the output is the HTTP code from -w.
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_HTTP_CODE="$HTTP_CODE"

    BODY=$(cat "$BODY_FILE" 2>/dev/null || echo "")
    [ -n "$BODY" ] && extract_body_info "$BODY"

    ACTUAL_HOSTNAME=$(resolve_actual_hostname "$OUTPUT")
    LAST_ACTUAL_HOSTNAME="$ACTUAL_HOSTNAME"

    if printf '%s' "$HTTP_CODE" | grep -Eq "$EXPECTED_STATUS_REGEX"; then
      validate_certificate "$ACTUAL_HOSTNAME"
      LAST_MESSAGE="$MESSAGE"

      if [ "$CERTIFICATE_MATCH" = "true" ]; then
        # Success! Status matched EXPECTED_STATUS_REGEX and certificate matches.
        emit_result \
          "num:http_status=$HTTP_CODE" \
          "bool:certificate_match=true" \
          "str:resolved_hostname=$ACTUAL_HOSTNAME" \
          "str:fqdn=$FQDN" \
          "str:method=$METHOD" \
          "str:message=$MESSAGE" \
          "str:pod_node=$POD_NODE" \
          "str:pod_name=$POD_NAME" \
          "str:pod_namespace=$POD_NAMESPACE" \
          "str:pod_ip=$POD_IP" \
          "num:retry_attempt=$ATTEMPT" \
          "num:max_retries=$MAX_RETRIES" \
          "str:curl_command=$CURL_CMD"
        exit 0
      fi
    fi

    # Either the status didn't match EXPECTED_STATUS_REGEX or the certificate
    # doesn't match, retry.
  else
    # Curl command failed.
    LAST_OUTPUT="$OUTPUT"
  fi

  [ "$ATTEMPT" -lt "$MAX_RETRIES" ] && sleep "$RETRY_DELAY"
done

# All retries exhausted, output failure.
if [ -z "$LAST_HTTP_CODE" ]; then
  # Curl never succeeded.
  emit_result \
    "bool:success=false" \
    "bool:certificate_match=false" \
    "str:error=Curl command failed after $MAX_RETRIES attempts" \
    "str:fqdn=$FQDN" \
    "str:proxy_ip=$PROXY_IP" \
    "str:method=$METHOD" \
    "str:route_path=$ROUTE_PATH" \
    "str:insecure=$INSECURE" \
    "num:retry_attempt=$MAX_RETRIES" \
    "num:max_retries=$MAX_RETRIES" \
    "str:curl_command=$CURL_CMD" \
    "str:curl_output=$LAST_OUTPUT"
elif ! printf '%s' "$LAST_HTTP_CODE" | grep -Eq "$EXPECTED_STATUS_REGEX"; then
  # Got an HTTP response, but the status didn't match EXPECTED_STATUS_REGEX.
  emit_result \
    "num:http_status=$LAST_HTTP_CODE" \
    "bool:certificate_match=false" \
    "str:method=$METHOD" \
    "str:error=Request failed with status $LAST_HTTP_CODE (expected to match $EXPECTED_STATUS_REGEX) after $MAX_RETRIES attempts" \
    "num:retry_attempt=$MAX_RETRIES" \
    "num:max_retries=$MAX_RETRIES" \
    "str:curl_command=$CURL_CMD" \
    "str:curl_output=$LAST_OUTPUT"
else
  # Status matched EXPECTED_STATUS_REGEX but certificate didn't match.
  emit_result \
    "num:http_status=$LAST_HTTP_CODE" \
    "bool:certificate_match=false" \
    "str:resolved_hostname=$LAST_ACTUAL_HOSTNAME" \
    "str:fqdn=$FQDN" \
    "str:method=$METHOD" \
    "str:message=$LAST_MESSAGE" \
    "str:pod_node=$POD_NODE" \
    "str:pod_name=$POD_NAME" \
    "str:pod_namespace=$POD_NAMESPACE" \
    "str:pod_ip=$POD_IP" \
    "str:error=Certificate hostname mismatch after $MAX_RETRIES attempts" \
    "num:retry_attempt=$MAX_RETRIES" \
    "num:max_retries=$MAX_RETRIES" \
    "str:curl_command=$CURL_CMD" \
    "str:curl_output=$LAST_OUTPUT"
fi
exit 1
