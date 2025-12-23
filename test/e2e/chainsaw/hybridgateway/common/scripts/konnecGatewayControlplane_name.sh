# abort on nonzero exitstatus.
set -o errexit
# abort on unbound variable.
set -o nounset
# abort on unbound variable.
set -o pipefail

# 1. Fetch values and store in shell variables
CP_NAME=$(kubectl get konnectgatewaycontrolplane -n "$NAMESPACE" -l gateway-operator.konghq.com/managed-by=gateway -o jsonpath='{.items[0].metadata.name}')
# 2. Final validation: Ensure critical variables are not empty
if [ -z "$CP_NAME" ]; then
  echo "Error: Required resources not found" >&2
  exit 1
fi

# 3. Output the JSON block for Chainsaw to parse
cat <<EOF
{
  "cp_name": "$CP_NAME"
}
EOF