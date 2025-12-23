# abort on nonzero exitstatus, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Required environment variables
GATEWAY_NAME="${GATEWAY_NAME:-}"
GATEWAY_NAMESPACE="${NAMESPACE:-}"

if [[ -z "$GATEWAY_NAME" || -z "$GATEWAY_NAMESPACE" ]]; then
  echo "Error: GATEWAY_NAME and GATEWAY_NAMESPACE must be set." >&2
  exit 1
fi

# Query for the KongCertificate name using labels.
# We use -o jsonpath to get the name of the first matching resource.
CERT_NAME=$(kubectl get kongcertificates.configuration.konghq.com -n "$GATEWAY_NAMESPACE" \
  -l "gateway-operator.konghq.com/hybrid-gateways-name=$GATEWAY_NAME" \
  -l "gateway-operator.konghq.com/hybrid-gateways-namespace=$GATEWAY_NAMESPACE" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

if [ -z "$CERT_NAME" ]; then
  echo "Error: No KongCertificate found for Gateway $GATEWAY_NAME" >&2
  exit 1
fi

printf '{"certificate_name":"%s"}\n' "$CERT_NAME"