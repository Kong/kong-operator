#!/bin/bash
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
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

FQDN="${FQDN}"
PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
INSECURE="${INSECURE:-true}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Determine insecure flag.
INSECURE_FLAG=""
if [[ "$INSECURE" == "true" ]]; then
  INSECURE_FLAG="--insecure"
fi

# Body temp file.
BODY_FILE=$(mktemp /tmp/curl_body.XXXXXX)

# Build curl command - capture body to temp file, output only HTTP code to stdout.
build_curl_cmd() {
  local CMD="curl -s -w '%{http_code}' -X $METHOD --resolve '${FQDN}:443:${PROXY_IP}' 'https://${FQDN}${ROUTE_PATH}' -vv $INSECURE_FLAG -o $BODY_FILE"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Function to extract info from response body.
# Body format:
#   Welcome, you are connected to node <node>.
#   Running on Pod <pod-name>.
#   In namespace <namespace>.
#   With IP address <ip>.
extract_body_info() {
  local BODY="$1"
  local POD_NODE=$(echo "$BODY" | grep -o 'connected to node [^.]*' | sed 's/connected to node //' || echo "")
  local POD_NAME=$(echo "$BODY" | grep -o 'Running on Pod [^.]*' | sed 's/Running on Pod //' || echo "")
  local POD_NAMESPACE=$(echo "$BODY" | grep -o 'In namespace [^.]*' | sed 's/In namespace //' || echo "")
  local POD_IP=$(echo "$BODY" | grep -o 'IP address .*' | sed 's/IP address //' || echo "")
  echo "${POD_NODE}|${POD_NAME}|${POD_NAMESPACE}|${POD_IP}"
}

# Function to validate certificate hostname.
validate_certificate() {
  local OUTPUT="$1"
  local ACTUAL_HOSTNAME="$2"

  # Verify that the certificate hostname matches or is a parent domain of the SNI hostname.
  # Supports:
  # 1. Exact match (echo.kong.example.com == echo.kong.example.com)
  # 2. Wildcard match (*.kong.example.com covers echo.kong.example.com)
  # 3. Parent domain match (kong.example.com covers echo.kong.example.com)

  if [[ "$ACTUAL_HOSTNAME" == "$FQDN" ]]; then
    # Exact match.
    echo "true|Certificate hostname validation passed: exact match $ACTUAL_HOSTNAME"
  elif [[ "$ACTUAL_HOSTNAME" == \*.* ]]; then
    # Wildcard certificate (e.g., *.kong.example.com).
    # Extract the base domain from the wildcard (remove the *.).
    WILDCARD_BASE="${ACTUAL_HOSTNAME#\*.}"
    # Check if SNI hostname ends with the wildcard base domain.
    if [[ "$FQDN" == *".$WILDCARD_BASE" ]] || [[ "$FQDN" == "$WILDCARD_BASE" ]]; then
      echo "true|Certificate hostname validation passed: wildcard $ACTUAL_HOSTNAME covers $FQDN"
    else
      echo "false|Certificate hostname mismatch: wildcard $ACTUAL_HOSTNAME does not cover $FQDN"
    fi
  elif [[ "$FQDN" == *".$ACTUAL_HOSTNAME" ]]; then
    # Parent domain match (e.g., kong.example.com covers echo.kong.example.com).
    echo "true|Certificate hostname validation passed: parent domain $ACTUAL_HOSTNAME covers $FQDN"
  else
    echo "false|Certificate hostname mismatch: expected $FQDN or a subdomain of $ACTUAL_HOSTNAME, got $ACTUAL_HOSTNAME"
  fi
}

# Retry loop: Keep trying until we get 200 and certificate matches, or run out of retries.
LAST_OUTPUT=""
LAST_HTTP_CODE=""
LAST_ACTUAL_HOSTNAME=""
LAST_MESSAGE=""
LAST_BODY_INFO=""

for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  # Clear body file.
  > $BODY_FILE

  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    LAST_OUTPUT="$OUTPUT"

    # The last line of the output is the HTTP code from -w.
    HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)
    LAST_HTTP_CODE="$HTTP_CODE"

    # Read response body.
    BODY=$(cat $BODY_FILE 2>/dev/null || echo "")
    if [[ -n "$BODY" ]]; then
      LAST_BODY_INFO=$(extract_body_info "$BODY")
    fi

    # Extract the Common Name (CN) from the 'subject:' line in the certificate section.
    # Look for pattern like "subject: ... CN=hostname" and extract the CN value.
    ACTUAL_HOSTNAME=$(echo "$OUTPUT" | grep -o 'subject:.*CN=[^;]*' | sed 's/.*CN=//' | tr -d ' ' || echo "")
    LAST_ACTUAL_HOSTNAME="$ACTUAL_HOSTNAME"

    if [[ "$HTTP_CODE" == "200" ]]; then
      # Check certificate validation.
      VALIDATION_RESULT=$(validate_certificate "$OUTPUT" "$ACTUAL_HOSTNAME")
      CERTIFICATE_MATCH=$(echo "$VALIDATION_RESULT" | cut -d'|' -f1)
      MESSAGE=$(echo "$VALIDATION_RESULT" | cut -d'|' -f2)
      LAST_MESSAGE="$MESSAGE"

      if [[ "$CERTIFICATE_MATCH" == "true" ]]; then
        # Success! Got 200 and certificate matches.
        POD_NODE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f1)
        POD_NAME=$(echo "$LAST_BODY_INFO" | cut -d'|' -f2)
        POD_NAMESPACE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f3)
        POD_IP=$(echo "$LAST_BODY_INFO" | cut -d'|' -f4)
        rm -f $BODY_FILE
        cat <<EOF
{
  "http_status": $HTTP_CODE,
  "certificate_match": true,
  "resolved_hostname": "$ACTUAL_HOSTNAME",
  "fqdn": "$FQDN",
  "method": "$METHOD",
  "message": "$MESSAGE",
  "pod_node": "$POD_NODE",
  "pod_name": "$POD_NAME",
  "pod_namespace": "$POD_NAMESPACE",
  "pod_ip": "$POD_IP",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "curl_command": "$CURL_CMD"
}
EOF
        exit 0
      fi
    fi

    # Either HTTP code is not 200 or certificate doesn't match, retry.
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  else
    # Curl command failed.
    LAST_OUTPUT="$OUTPUT"
    if [[ $ATTEMPT -lt $MAX_RETRIES ]]; then
      sleep $RETRY_DELAY
    fi
  fi
done

# All retries exhausted, output failure.
rm -f $BODY_FILE
if [[ -z "$LAST_HTTP_CODE" ]]; then
  # Curl never succeeded.
  cat <<EOF
{
  "success": false,
  "error": "Curl command failed after $MAX_RETRIES attempts",
  "fqdn": "$FQDN",
  "proxy_ip": "$PROXY_IP",
  "method": "$METHOD",
  "route_path": "$ROUTE_PATH",
  "insecure": "$INSECURE",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .)
}
EOF
elif [[ "$LAST_HTTP_CODE" != "200" ]]; then
  # Got HTTP response but not 200.
  cat <<EOF
{
  "http_status": $LAST_HTTP_CODE,
  "certificate_match": false,
  "method": "$METHOD",
  "error": "Request failed with status $LAST_HTTP_CODE after $MAX_RETRIES attempts",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .)
}
EOF
else
  # Got 200 but certificate didn't match.
  POD_NODE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f1)
  POD_NAME=$(echo "$LAST_BODY_INFO" | cut -d'|' -f2)
  POD_NAMESPACE=$(echo "$LAST_BODY_INFO" | cut -d'|' -f3)
  POD_IP=$(echo "$LAST_BODY_INFO" | cut -d'|' -f4)
  cat <<EOF
{
  "http_status": $LAST_HTTP_CODE,
  "certificate_match": false,
  "resolved_hostname": "$LAST_ACTUAL_HOSTNAME",
  "fqdn": "$FQDN",
  "method": "$METHOD",
  "message": "$LAST_MESSAGE",
  "pod_node": "$POD_NODE",
  "pod_name": "$POD_NAME",
  "pod_namespace": "$POD_NAMESPACE",
  "pod_ip": "$POD_IP",
  "error": "Certificate hostname mismatch after $MAX_RETRIES attempts",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .)
}
EOF
fi
exit 1
