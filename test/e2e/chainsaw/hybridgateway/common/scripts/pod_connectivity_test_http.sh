#!/bin/bash
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   SCRIPT_PATH: (optional) Path to the connectivity test script. Default: ../common/scripts/host_connectivity_test_http.sh
#   POD_NAMESPACE: (optional) Namespace for the test pod. Default: 'default'.
#   SCRIPT_PATH: (optional) Path to the connectivity test script. Default: ../common/scripts/host_connectivity_test_http.sh
#   POD_NAMESPACE: (optional) Namespace for the test pod. Default: 'default'.
#   PROXY_IP: The IP address of the proxy to connect to.
#   METHOD: The HTTP method to use (e.g., 'GET', 'POST', 'PUT').
#   ROUTE_PATH: (optional) The HTTP path to test. Default: '/'.
#   PROXY_PORT: (optional) The port to connect to. Default: '80'.
#   HOST: (optional) The Host header to send with the request.

SCRIPT_PATH="${SCRIPT_PATH:-../common/scripts/host_connectivity_test_http.sh}"
POD_NAMESPACE="${POD_NAMESPACE:-default}"
POD_NAME="chainsaw-cluster-test-$(date +%s)"
PROXY_IP="${PROXY_IP}"
METHOD="${METHOD}"
ROUTE_PATH="${ROUTE_PATH:-/}"
PROXY_PORT="${PROXY_PORT:-80}"
HOST="${HOST:-}"

# 1. Start a pod running bash
# 2. Set the env vars in the pod
# 3. Pipe the local script into the pod's stdin
cat "$SCRIPT_PATH" | kubectl run "$POD_NAME" \
  --image=curlimages/curl:latest \
  --rm -i --restart=Never \
  --env="PROXY_IP=$PROXY_IP" \
  --env="ROUTE_PATH=$ROUTE_PATH" \
  --env="PROXY_PORT=$PROXY_PORT" \
  --env="HOST=$HOST" \
  --env="METHOD=$METHOD" \
  -n "$POD_NAMESPACE" \
  -- sh -s 2>/dev/null | grep -v '^pod "'
