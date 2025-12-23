#!/bin/bash
# abort on nonzero exitstatus, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Required env vars: SNI_HOSTNAME, PROXY_IP, ROUTE_PATH
SNI_HOSTNAME="${SNI_HOSTNAME:-}"
PROXY_IP="${PROXY_IP:-}"
ROUTE_PATH="${ROUTE_PATH:-/}"
INSECURE="${INSECURE:-true}"

if [[ -z "$SNI_HOSTNAME" || -z "$PROXY_IP" ]]; then
  echo "Error: SNI_HOSTNAME and PROXY_IP must be set" >&2
  exit 1
fi

# Run curl, merging stderr into stdout to capture TLS handshake info
INSECURE_FLAG=""
if [[ "$INSECURE" == "true" ]]; then
  INSECURE_FLAG="--insecure"
fi

OUTPUT=$(curl --fail --retry 10 --retry-delay 5 --retry-all-errors -s -o /dev/null -w '%{http_code}' \
  --resolve "${SNI_HOSTNAME}:443:${PROXY_IP}" \
  "https://${SNI_HOSTNAME}${ROUTE_PATH}" \
  -vv $INSECURE_FLAG 2>&1)

# The last line of the output is the HTTP code from -w
HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)

# Extract the Common Name (CN) from the 'subject:' line in the certificate section
# Look for pattern like "subject: ... CN=hostname" and extract the CN value
ACTUAL_HOSTNAME=$(echo "$OUTPUT" | grep -o 'subject:.*CN=[^;]*' | sed 's/.*CN=//' | tr -d ' ')

# Also extract Subject Alternative Names if present
SANS=$(echo "$OUTPUT" | grep -o 'subjectAltName:.*' || echo "No SANs found")

# Validation for Chainsaw 'check' block or manual exit
CERTIFICATE_MATCH=false

if [[ "$HTTP_CODE" != "200" ]]; then
  echo "{\"httpStatus\":$HTTP_CODE,\"certificateMatch\":false,\"error\":\"Request failed with status $HTTP_CODE\"}"
  exit 1
fi

# Verify that the certificate hostname matches or is a parent domain of the SNI hostname
# Supports:
# 1. Exact match (echo.kong.example.com == echo.kong.example.com)
# 2. Wildcard match (*.kong.example.com covers echo.kong.example.com)
# 3. Parent domain match (kong.example.com covers echo.kong.example.com)

if [[ "$ACTUAL_HOSTNAME" == "$SNI_HOSTNAME" ]]; then
  # Exact match
  CERTIFICATE_MATCH=true
  MESSAGE="Certificate hostname validation passed: exact match $ACTUAL_HOSTNAME"
elif [[ "$ACTUAL_HOSTNAME" == \*.* ]]; then
  # Wildcard certificate (e.g., *.kong.example.com)
  # Extract the base domain from the wildcard (remove the *.)
  WILDCARD_BASE="${ACTUAL_HOSTNAME#\*.}"
  # Check if SNI hostname ends with the wildcard base domain
  if [[ "$SNI_HOSTNAME" == *".$WILDCARD_BASE" ]] || [[ "$SNI_HOSTNAME" == "$WILDCARD_BASE" ]]; then
    CERTIFICATE_MATCH=true
    MESSAGE="Certificate hostname validation passed: wildcard $ACTUAL_HOSTNAME covers $SNI_HOSTNAME"
  else
    MESSAGE="Certificate hostname mismatch: wildcard $ACTUAL_HOSTNAME does not cover $SNI_HOSTNAME"
  fi
elif [[ "$SNI_HOSTNAME" == *".$ACTUAL_HOSTNAME" ]]; then
  # Parent domain match (e.g., kong.example.com covers echo.kong.example.com)
  CERTIFICATE_MATCH=true
  MESSAGE="Certificate hostname validation passed: parent domain $ACTUAL_HOSTNAME covers $SNI_HOSTNAME"
else
  MESSAGE="Certificate hostname mismatch: expected $SNI_HOSTNAME or a subdomain of $ACTUAL_HOSTNAME, got $ACTUAL_HOSTNAME"
fi

# Output JSON result
echo "{\"httpStatus\":$HTTP_CODE,\"certificateMatch\":$CERTIFICATE_MATCH,\"resolvedHostname\":\"$ACTUAL_HOSTNAME\",\"sniHostname\":\"$SNI_HOSTNAME\",\"message\":\"$MESSAGE\"}"

# Exit with error if certificate doesn't match
if [[ "$CERTIFICATE_MATCH" != "true" ]]; then
  exit 1
fi



