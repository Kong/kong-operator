# abort on nonzero exitstatus, unbound variable, and pipefail.
set -o errexit
set -o nounset
set -o pipefail

# Required environment variables.
GATEWAY_NAME="${GATEWAY_NAME:-}"
GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE:-}"

if [[ -z "$GATEWAY_NAME" || -z "$GATEWAY_NAMESPACE" ]]; then
  echo "Usage: GATEWAY_NAME and GATEWAY_NAMESPACE must be set" >&2
  exit 1
fi

PROXY_IP_ADDRESS=$(kubectl get gateway ${GATEWAY_NAME} -n ${GATEWAY_NAMESPACE} -o jsonpath='{.status.addresses[0].value}')

if [[ -z "$PROXY_IP_ADDRESS" || "$PROXY_IP_ADDRESS" == "null" || "$PROXY_IP_ADDRESS" == "" ]]; then
  echo "Error: No proxy IP address found for gateway ${GATEWAY_NAMESPACE}/${GATEWAY_NAME}" >&2
  exit 1
fi

printf '{"proxy_ip_address":"%s"}\n' "$PROXY_IP_ADDRESS"