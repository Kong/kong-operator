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

FQDN="${FQDN}"
PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
INSECURE="${INSECURE:-true}"

# Run curl, merging stderr into stdout to capture TLS handshake info
INSECURE_FLAG=""
if [[ "$INSECURE" == "true" ]]; then
  INSECURE_FLAG="--insecure"
fi

# Capture curl output, and handle failures gracefully
if ! OUTPUT=$(curl --fail --retry 10 --retry-delay 5 --retry-all-errors -s -o /dev/null -w '%{http_code}' \
  -X "$METHOD" \
  --resolve "${FQDN}:443:${PROXY_IP}" \
  "https://${FQDN}${ROUTE_PATH}" \
  -vv $INSECURE_FLAG 2>&1); then
  # Curl failed - output the full debug info
  cat <<EOF
{
  "error": "Curl command failed",
  "fqdn": "$FQDN",
  "proxy_ip": "$PROXY_IP",
  "method": "$METHOD",
  "route_path": "$ROUTE_PATH",
  "insecure": "$INSECURE",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# The last line of the output is the HTTP code from -w
HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)

# Extract the Common Name (CN) from the 'subject:' line in the certificate section
# Look for pattern like "subject: ... CN=hostname" and extract the CN value
ACTUAL_HOSTNAME=$(echo "$OUTPUT" | grep -o 'subject:.*CN=[^;]*' | sed 's/.*CN=//' | tr -d ' ' || echo "")

# Also extract Subject Alternative Names if present
SANS=$(echo "$OUTPUT" | grep -o 'subjectAltName:.*' || echo "No SANs found")

# Validation for Chainsaw 'check' block or manual exit
CERTIFICATE_MATCH=false

if [[ "$HTTP_CODE" != "200" ]]; then
  cat <<EOF
{
  "http_status": $HTTP_CODE,
  "certificate_match": false,
  "method": "$METHOD",
  "error": "Request failed with status $HTTP_CODE",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# Verify that the certificate hostname matches or is a parent domain of the SNI hostname
# Supports:
# 1. Exact match (echo.kong.example.com == echo.kong.example.com)
# 2. Wildcard match (*.kong.example.com covers echo.kong.example.com)
# 3. Parent domain match (kong.example.com covers echo.kong.example.com)

if [[ "$ACTUAL_HOSTNAME" == "$FQDN" ]]; then
  # Exact match
  CERTIFICATE_MATCH=true
  MESSAGE="Certificate hostname validation passed: exact match $ACTUAL_HOSTNAME"
elif [[ "$ACTUAL_HOSTNAME" == \*.* ]]; then
  # Wildcard certificate (e.g., *.kong.example.com)
  # Extract the base domain from the wildcard (remove the *.)
  WILDCARD_BASE="${ACTUAL_HOSTNAME#\*.}"
  # Check if SNI hostname ends with the wildcard base domain
  if [[ "$FQDN" == *".$WILDCARD_BASE" ]] || [[ "$FQDN" == "$WILDCARD_BASE" ]]; then
    CERTIFICATE_MATCH=true
    MESSAGE="Certificate hostname validation passed: wildcard $ACTUAL_HOSTNAME covers $FQDN"
  else
    MESSAGE="Certificate hostname mismatch: wildcard $ACTUAL_HOSTNAME does not cover $FQDN"
  fi
elif [[ "$FQDN" == *".$ACTUAL_HOSTNAME" ]]; then
  # Parent domain match (e.g., kong.example.com covers echo.kong.example.com)
  CERTIFICATE_MATCH=true
  MESSAGE="Certificate hostname validation passed: parent domain $ACTUAL_HOSTNAME covers $FQDN"
else
  MESSAGE="Certificate hostname mismatch: expected $FQDN or a subdomain of $ACTUAL_HOSTNAME, got $ACTUAL_HOSTNAME"
fi

# Output JSON result
cat <<EOF
{
  "http_status": $HTTP_CODE,
  "certificate_match": $CERTIFICATE_MATCH,
  "resolved_hostname": "$ACTUAL_HOSTNAME",
  "fqdn": "$FQDN",
  "method": "$METHOD",
  "message": "$MESSAGE"
}
EOF

# Exit with error if certificate doesn't match
if [[ "$CERTIFICATE_MATCH" != "true" ]]; then
  exit 1
fi
