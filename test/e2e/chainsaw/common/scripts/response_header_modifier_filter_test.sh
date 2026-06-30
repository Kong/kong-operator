#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test.
#   PROXY_PORT: The port to connect to.
#   HOST: The Host header to send with the request.
#   HEADERS_TO_SEND: Headers to send with the request, format: 'Header1:value1,Header2:value2'.
#   EXPECTED_HEADERS_PRESENT: Response headers that must be present, format: 'Header1:value1,Header2:value2'.
#   EXPECTED_HEADERS_ABSENT: Response headers that must NOT be present, format: 'Header1,Header2'.
#   PROTOCOL: Protocol to use: 'http' or 'https'.
#   INSECURE: Set to 'true' to use --insecure flag for HTTPS with self-signed certificates, 'false' otherwise.
#   MAX_RETRIES: (optional) Maximum number of retry attempts. Default: '180'.
#   RETRY_DELAY: (optional) Delay in seconds between retries. Default: '1'.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
PROXY_PORT="${PROXY_PORT}"
HOST="${HOST}"
HEADERS_TO_SEND="${HEADERS_TO_SEND}"
EXPECTED_HEADERS_PRESENT="${EXPECTED_HEADERS_PRESENT}"
EXPECTED_HEADERS_ABSENT="${EXPECTED_HEADERS_ABSENT}"
PROTOCOL="${PROTOCOL}"
INSECURE="${INSECURE}"

# Retry configuration (configurable via environment variables).
# Default: 180 retries with 1 second delay = up to 180 seconds total.
MAX_RETRIES="${MAX_RETRIES:-180}"
RETRY_DELAY="${RETRY_DELAY:-1}"

# Determine insecure flag.
if [[ "$INSECURE" == "true" ]]; then
  INSECURE_FLAG="--insecure"
else
  INSECURE_FLAG=""
fi

# Build curl command with -v to capture response headers in verbose output.
build_curl_cmd() {
  local CMD="curl -v $INSECURE_FLAG "
  if [[ -n "$HOST" ]]; then
    CMD="$CMD -H 'Host: $HOST' "
  fi

  # Add custom headers to send.
  if [[ -n "$HEADERS_TO_SEND" ]]; then
    IFS=',' read -ra HEADERS <<< "$HEADERS_TO_SEND"
    for HEADER in "${HEADERS[@]}"; do
      HEADER=$(echo "$HEADER" | xargs)
      CMD="$CMD -H '$HEADER' "
    done
  fi

  CMD="$CMD '${PROTOCOL}://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"
  echo "$CMD"
}

CURL_CMD=$(build_curl_cmd)

# Function to validate response headers.
validate_headers() {
  local RESPONSE_HEADERS="$1"
  local HEADER_ERRORS=""

  # Validation: Check for expected headers that MUST be present in response.
  if [[ -n "$EXPECTED_HEADERS_PRESENT" ]]; then
    IFS=',' read -ra EXPECTED_PRESENT <<< "$EXPECTED_HEADERS_PRESENT"
    for HEADER_CHECK in "${EXPECTED_PRESENT[@]}"; do
      HEADER_CHECK=$(echo "$HEADER_CHECK" | xargs)

      if [[ "$HEADER_CHECK" == *":"* ]]; then
        HEADER_NAME=$(echo "$HEADER_CHECK" | cut -d: -f1 | xargs)
        EXPECTED_VALUE=$(echo "$HEADER_CHECK" | cut -d: -f2- | xargs)

        # Check if header with expected value exists in response (case-insensitive).
        if ! echo "$RESPONSE_HEADERS" | grep -qi "^${HEADER_NAME}:.*${EXPECTED_VALUE}"; then
          HEADER_ERRORS="${HEADER_ERRORS}Expected response header '$HEADER_NAME: $EXPECTED_VALUE' not found. "
        fi
      else
        HEADER_NAME=$(echo "$HEADER_CHECK" | xargs)

        # Check if header exists (presence only).
        if ! echo "$RESPONSE_HEADERS" | grep -qi "^${HEADER_NAME}:"; then
          HEADER_ERRORS="${HEADER_ERRORS}Expected response header '$HEADER_NAME' not found. "
        fi
      fi
    done
  fi

  # Validation: Check for headers that MUST be absent in response.
  if [[ -n "$EXPECTED_HEADERS_ABSENT" ]]; then
    IFS=',' read -ra EXPECTED_ABSENT <<< "$EXPECTED_HEADERS_ABSENT"
    for HEADER_NAME in "${EXPECTED_ABSENT[@]}"; do
      HEADER_NAME=$(echo "$HEADER_NAME" | xargs)

      # Check if header is present (it shouldn't be).
      if echo "$RESPONSE_HEADERS" | grep -qi "^${HEADER_NAME}:"; then
        HEADER_ERRORS="${HEADER_ERRORS}Response header '$HEADER_NAME' should have been removed but was found. "
      fi
    done
  fi

  echo "$HEADER_ERRORS"
}

# Retry loop: Keep trying until validation passes or run out of retries.
LAST_OUTPUT=""
LAST_RESPONSE_HEADERS=""
LAST_HEADER_ERRORS=""

for ATTEMPT in $(seq 1 $MAX_RETRIES); do
  if OUTPUT=$(eval $CURL_CMD 2>&1); then
    LAST_OUTPUT="$OUTPUT"

    # Extract response headers from verbose output (lines starting with '< ').
    # Remove the '< ' prefix to get clean header lines.
    RESPONSE_HEADERS=$(echo "$OUTPUT" | grep '^< ' | sed 's/^< //')
    LAST_RESPONSE_HEADERS="$RESPONSE_HEADERS"

    # Validate headers.
    HEADER_ERRORS=$(validate_headers "$RESPONSE_HEADERS")
    LAST_HEADER_ERRORS="$HEADER_ERRORS"

    if [[ -z "$HEADER_ERRORS" ]]; then
      # Success! All validations passed.
      cat <<EOF
{
  "success": true,
  "message": "ResponseHeaderModifier validation successful",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "retry_attempt": $ATTEMPT,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "validation": {
    "headers_sent": "$HEADERS_TO_SEND",
    "expected_present": "$EXPECTED_HEADERS_PRESENT",
    "expected_absent": "$EXPECTED_HEADERS_ABSENT"
  }
}
EOF
      exit 0
    fi

    # Validation failed, retry.
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
cat <<EOF
{
  "success": false,
  "error": "ResponseHeaderModifier validation failed after $MAX_RETRIES attempts",
  "header_errors": "$LAST_HEADER_ERRORS",
  "headers_sent": "$HEADERS_TO_SEND",
  "expected_present": "$EXPECTED_HEADERS_PRESENT",
  "expected_absent": "$EXPECTED_HEADERS_ABSENT",
  "retry_attempt": $MAX_RETRIES,
  "max_retries": $MAX_RETRIES,
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$LAST_OUTPUT" | jq -Rs .),
  "response_headers": $(echo "$LAST_RESPONSE_HEADERS" | jq -Rs .)
}
EOF
exit 1
