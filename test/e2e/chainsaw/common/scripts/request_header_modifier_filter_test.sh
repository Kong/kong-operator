#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: The HTTP path to test. Must be an endpoint that echoes headers (e.g., /headers for httpbin, / for go-echo).
#   PROXY_PORT: The port to connect to.
#   HOST: The Host header to send with the request.
#   HEADERS_TO_SEND: Headers to send with the request, format: 'Header1:value1,Header2:value2'.
#   EXPECTED_HEADERS_PRESENT: Headers that must be present in the backend response, format: 'Header1:value1,Header2:value2'.
#   EXPECTED_HEADERS_ABSENT: Headers that must NOT be present in the backend response, format: 'Header1,Header2'.
#   PROTOCOL: Protocol to use: 'http' or 'https'.
#   INSECURE: Set to 'true' to use --insecure flag for HTTPS with self-signed certificates, 'false' otherwise.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH}"
PROXY_PORT="${PROXY_PORT}"
HOST="${HOST}"
HEADERS_TO_SEND="${HEADERS_TO_SEND}"
EXPECTED_HEADERS_PRESENT="${EXPECTED_HEADERS_PRESENT}"
EXPECTED_HEADERS_ABSENT="${EXPECTED_HEADERS_ABSENT}"
PROTOCOL="${PROTOCOL}"
INSECURE="${INSECURE}"

# Determine insecure flag.
if [[ "$INSECURE" == "true" ]]; then
  INSECURE_FLAG="--insecure"
else
  INSECURE_FLAG=""
fi

# Build curl command.
CURL_CMD="curl --fail --retry 10 --retry-delay 5 --retry-all-errors -v -s -S $INSECURE_FLAG "
if [[ -n "$HOST" ]]; then
  CURL_CMD="$CURL_CMD -H 'Host: $HOST' "
fi

# Add custom headers to send.
if [[ -n "$HEADERS_TO_SEND" ]]; then
  IFS=',' read -ra HEADERS <<< "$HEADERS_TO_SEND"
  for HEADER in "${HEADERS[@]}"; do
    HEADER=$(echo "$HEADER" | xargs)
    CURL_CMD="$CURL_CMD -H '$HEADER' "
  done
fi

CURL_CMD="$CURL_CMD '${PROTOCOL}://${PROXY_IP}:${PROXY_PORT}${ROUTE_PATH}'"

# Capture curl output (stdout and stderr).
if ! OUTPUT=$(eval $CURL_CMD 2>&1); then
  cat <<EOF
{
  "error": "Curl command failed",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT", 
  "route_path": "$ROUTE_PATH",
  "host_header": "$HOST",
  "headers_sent": "$HEADERS_TO_SEND",
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# Extract just the JSON response body (after the blank line from headers).
RESPONSE=$(echo "$OUTPUT" | awk '/^\{/,0')

# Parse the response to extract headers received by the backend.
# This assumes the backend is an echo service that returns headers in JSON format
# For httpbin: {"headers": {"X-Set": "foo", ...}}.
# For go-echo: includes headers in the response.

# Validation: Check for expected headers that MUST be present.
HEADER_ERRORS=""
if [[ -n "$EXPECTED_HEADERS_PRESENT" ]]; then
  IFS=',' read -ra EXPECTED_PRESENT <<< "$EXPECTED_HEADERS_PRESENT"
  for HEADER_CHECK in "${EXPECTED_PRESENT[@]}"; do
    HEADER_CHECK=$(echo "$HEADER_CHECK" | xargs)
    
    if [[ "$HEADER_CHECK" == *":"* ]]; then
      HEADER_NAME=$(echo "$HEADER_CHECK" | cut -d: -f1 | xargs)
      EXPECTED_VALUE=$(echo "$HEADER_CHECK" | cut -d: -f2- | xargs)
      
      # Check if header with expected value exists in response (case-insensitive).
      # Handle JSON array format: "Header": ["value"].
      # Use tr to handle newlines, making grep work across lines.
      if ! echo "$RESPONSE" | tr '\n' ' ' | grep -qi "\"${HEADER_NAME}\"[^}]*\"${EXPECTED_VALUE}\""; then
        HEADER_ERRORS="${HEADER_ERRORS}Expected header '$HEADER_NAME: $EXPECTED_VALUE' not found in backend response. "
      fi
    else
      HEADER_NAME=$(echo "$HEADER_CHECK" | xargs)
      
      # Check if header exists (presence only).
      if ! echo "$RESPONSE" | tr '\n' ' ' | grep -qi "\"${HEADER_NAME}\""; then
        HEADER_ERRORS="${HEADER_ERRORS}Expected header '$HEADER_NAME' not found in backend response. "
      fi
    fi
  done
fi

# Validation: Check for headers that MUST be absent.
if [[ -n "$EXPECTED_HEADERS_ABSENT" ]]; then
  IFS=',' read -ra EXPECTED_ABSENT <<< "$EXPECTED_HEADERS_ABSENT"
  for HEADER_NAME in "${EXPECTED_ABSENT[@]}"; do
    HEADER_NAME=$(echo "$HEADER_NAME" | xargs)
    
    # Check if header is present (it shouldn't be).
    if echo "$RESPONSE" | tr '\n' ' ' | grep -qi "\"${HEADER_NAME}\""; then
      HEADER_ERRORS="${HEADER_ERRORS}Header '$HEADER_NAME' should have been removed but was found in backend response. "
    fi
  done
fi

if [[ -n "$HEADER_ERRORS" ]]; then
  cat <<EOF
{
  "error": "RequestHeaderModifier validation failed",
  "header_errors": "$HEADER_ERRORS",
  "headers_sent": "$HEADERS_TO_SEND",
  "expected_present": "$EXPECTED_HEADERS_PRESENT",
  "expected_absent": "$EXPECTED_HEADERS_ABSENT",
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

cat <<EOF
{
  "success": true,
  "message": "RequestHeaderModifier validation successful",
  "proxy_ip": "$PROXY_IP",
  "proxy_port": "$PROXY_PORT",
  "route_path": "$ROUTE_PATH",
  "curl_command": $(echo "$CURL_CMD" | jq -Rs .),
  "validation": {
    "headers_sent": "$HEADERS_TO_SEND",
    "expected_present": "$EXPECTED_HEADERS_PRESENT",
    "expected_absent": "$EXPECTED_HEADERS_ABSENT"
  }
}
EOF
