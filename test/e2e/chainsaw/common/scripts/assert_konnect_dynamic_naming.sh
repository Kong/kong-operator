#!/bin/bash
# Assert the invariants of the default (dynamic) Gateway naming path:
#   - the Konnect Control Plane name equals the generated Kubernetes name
#     (https://github.com/Kong/kong-operator/issues/3357);
#   - the Konnect name is not qualified with the Gateway namespace (the fix in
#     https://github.com/Kong/kong-operator/issues/4079 qualifies it only under static naming);
#   - the generated Kubernetes name carries a random suffix, i.e. it is not the bare Gateway
#     name that static naming would produce.
#
# A single read, not a retry loop: the caller is expected to have waited for the
# KonnectGatewayControlPlane to exist (e.g. an assert step on its Programmed status).
#
# Abort on nonzero exit status, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   NAMESPACE: The namespace of the Gateway and its KonnectGatewayControlPlane.
#   GATEWAY_NAME: The name of the Gateway.

NAMESPACE="${NAMESPACE}"
GATEWAY_NAME="${GATEWAY_NAME}"

SELECTOR="gateway-operator.konghq.com/managed-by=gateway,gateway.networking.k8s.io/gateway-name=${GATEWAY_NAME}"
KUBECTL_CMD="kubectl get konnectgatewaycontrolplanes.konnect.konghq.com -n ${NAMESPACE} -l ${SELECTOR} -o json"

K8S_NAME=""
KONNECT_NAME=""

fail() {
  cat <<EOF
{
  "success": false,
  "error": "$1",
  "namespace": "$NAMESPACE",
  "gateway_name": "$GATEWAY_NAME",
  "kubernetes_name": "$K8S_NAME",
  "konnect_name": "$KONNECT_NAME",
  "kubectl_command": "$KUBECTL_CMD"
}
EOF
  exit 1
}

if ! KUBECTL_OUTPUT=$(kubectl get konnectgatewaycontrolplanes.konnect.konghq.com \
  -n "$NAMESPACE" -l "$SELECTOR" -o json 2>&1); then
  fail "failed to list the Gateway's KonnectGatewayControlPlanes: $(echo "$KUBECTL_OUTPUT" | jq -Rs .)"
fi

ITEM_COUNT=$(echo "$KUBECTL_OUTPUT" | jq '.items | length')
if [ "$ITEM_COUNT" -ne 1 ]; then
  fail "expected exactly one KonnectGatewayControlPlane for the Gateway, found ${ITEM_COUNT}"
fi

K8S_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r '.items[0].metadata.name // ""')
KONNECT_NAME=$(echo "$KUBECTL_OUTPUT" | jq -r '.items[0].spec.createControlPlaneRequest.name // ""')

if [ -z "$K8S_NAME" ] || [ -z "$KONNECT_NAME" ]; then
  fail "could not read both names from the KonnectGatewayControlPlane"
fi

# On the dynamic path the two names are the same string
# (https://github.com/Kong/kong-operator/issues/3357).
if [ "$K8S_NAME" != "$KONNECT_NAME" ]; then
  fail "the Konnect name must equal the Kubernetes name on the dynamic naming path (#3357)"
fi

# The fix in https://github.com/Kong/kong-operator/issues/4079 qualifies the Konnect name
# ONLY under static naming.
if [ "$KONNECT_NAME" = "${NAMESPACE}_${GATEWAY_NAME}" ]; then
  fail "the dynamic Konnect name must not be qualified with the namespace"
fi

# Dynamic naming appends a random suffix.
if [ "$K8S_NAME" = "$GATEWAY_NAME" ]; then
  fail "the dynamic Kubernetes name must carry a random suffix"
fi

cat <<EOF
{
  "success": true,
  "namespace": "$NAMESPACE",
  "gateway_name": "$GATEWAY_NAME",
  "kubernetes_name": "$K8S_NAME",
  "konnect_name": "$KONNECT_NAME"
}
EOF
