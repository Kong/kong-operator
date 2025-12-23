# abort on nonzero exitstatus.
set -o errexit
# abort on unbound variable.
set -o nounset
# abort on unbound variable.
set -o pipefail

# 1. Fetch values and store in shell variables
# We use '|| exit 1' to be explicit, though set -e handles this
CP_NAME=$(kubectl get konnectgatewaycontrolplane -n kong -l gateway-operator.konghq.com/managed-by=gateway -o jsonpath='{.items[0].metadata.name}')
GW_NAME=$(kubectl get gateway -n kong -o jsonpath='{.items[0].metadata.name}')
GW_NS=$(kubectl get gateway -n kong -o jsonpath='{.items[0].metadata.namespace}')
SEC_NS=$(kubectl get secret test-tls-secret -n kong -o jsonpath='{.metadata.namespace}')

# For SNI and Listener, we handle potential empty results carefully
SNI=$(kubectl get gateway -n kong -o jsonpath='{.items[0].spec.listeners[?(@.protocol=="HTTPS")].hostname}' | tr -d '[]"' || echo "")
L_NAME=$(kubectl get gateway -n kong -o jsonpath='{.items[0].spec.listeners[?(@.protocol=="HTTPS")].name}' | head -n1 || echo "")

# 2. Final validation: Ensure critical variables are not empty
if [ -z "$CP_NAME" ] || [ -z "$GW_NAME" ]; then
  echo "Error: Required resources not found" >&2
  exit 1
fi

# 3. Output the JSON block for Chainsaw to parse
cat <<EOF
{
  "cp_name": "$CP_NAME",
  "gw_name": "$GW_NAME",
  "gw_ns": "$GW_NS",
  "sec_ns": "$SEC_NS",
  "sni": "$SNI",
  "l_name": "$L_NAME"
}
EOF
