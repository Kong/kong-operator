#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.

PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH:-/}"

# Capture curl output, and handle failures gracefully
if ! OUTPUT=$(curl --fail --retry 10 --retry-delay 5 --retry-all-errors -s -o /dev/null -w '%{http_code}' "http://${PROXY_IP}${ROUTE_PATH}")
  then
  # Curl failed - output the full debug info
  cat <<EOF
{
  "error": "Curl command failed",
  "proxy_ip": "$PROXY_IP",
  "route_path": "$ROUTE_PATH",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# The last line of the output is the HTTP code from -w
HTTP_CODE=$(echo "$OUTPUT" | tail -n 1)

if [[ "$HTTP_CODE" != "200" ]]; then
  cat <<EOF
{
  "http_status": $HTTP_CODE,
  "certificate_match": false,
  "error": "Request failed with status $HTTP_CODE",
  "curl_output": $(echo "$OUTPUT" | jq -Rs .)
}
EOF
  exit 1
fi

# Output JSON result
cat <<EOF
{
  "http_status": $HTTP_CODE
}
EOF
