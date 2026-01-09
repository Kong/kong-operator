#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   SCRIPT_PATH: (optional) Path to the connectivity test script. Default: ../common/scripts/host_connectivity_test_with_hostname_tls_check.sh
#   POD_NAMESPACE: (optional) Namespace for the test pod. Default: 'default'.
#   FQDN: The fully qualified domain name to test.
#   PROXY_IP: The IP address of the proxy to connect to.
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.
#   INSECURE: (optional) If 'true', disables TLS verification. Default: 'true'.

SCRIPT_PATH="${SCRIPT_PATH:-../common/scripts/host_connectivity_test_with_hostname_tls_check.sh}"
POD_NAMESPACE="${POD_NAMESPACE:-default}"
POD_NAME="chainsaw-cluster-test-$(date +%s)"
FQDN="${FQDN}"
PROXY_IP="${PROXY_IP}"
ROUTE_PATH="${ROUTE_PATH:-/}"
INSECURE="${INSECURE:-true}"

# 1. Start a pod running bash
# 2. Set the env vars in the pod
# 3. Pipe the local script into the pod's stdin
cat "$SCRIPT_PATH" | kubectl run "$POD_NAME" \
  --image=curlimages/curl:latest \
  --rm -i --restart=Never \
  --env="FQDN=$FQDN" \
  --env="PROXY_IP=$PROXY_IP" \
  --env="ROUTE_PATH=$ROUTE_PATH" \
  --env="INSECURE=$INSECURE" \
  -n "$POD_NAMESPACE" \
  -- sh -s 2>/dev/null | grep -v '^pod "'
