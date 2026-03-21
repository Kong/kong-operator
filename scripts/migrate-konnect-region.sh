#!/usr/bin/env bash

# ---------------------------------------------------------------------------
# Migrates DataPlanes managed by Kong Operator from one Konnect region to another (eu <-> me).
# It walks the resource chain from KonnectAPIAuthConfiguration -> KonnectGatewayControlPlane(s)
# -> KonnectExtension(s) -> DataPlane(s) -> Deployment(s), patches all region-specific endpoints,
# and temporarily scales down the operator to prevent it from reverting the changes mid-migration.
# ---------------------------------------------------------------------------
set -euo pipefail

# ---------------------------------------------------------------------------
# Colors and logging helpers
# ---------------------------------------------------------------------------

BOLD='\033[1m'
RESET='\033[0m'
WHITE='\033[1;37m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
RED='\033[0;31m'
GRAY='\033[0;90m'

log_info()    { echo -e "${WHITE}[INFO]${RESET}    $*"; }
log_resolve() { echo -e "${BLUE}[RESOLVE]${RESET}  $*"; }
log_patch()   { echo -e "${YELLOW}[PATCH]${RESET}   $*"; }
log_scale()   { echo -e "${MAGENTA}[SCALE]${RESET}   $*"; }
log_wait()    { echo -e "${CYAN}[WAIT]${RESET}    $*"; }
log_ok()      { echo -e "${GREEN}[OK]${RESET}      $*"; }
log_error()   { echo -e "${RED}[ERROR]${RESET}   $*" >&2; }
log_detail()  { echo -e "${GRAY}           $*${RESET}"; }

usage() {
  local exit_code="${1:-1}"
  echo -e "${BOLD}Usage:${RESET} $0 --region-from <region> --region-to <region> --auth-config-name <name> --namespace <namespace>"
  echo ""
  echo -e "${BOLD}Flags:${RESET}"
  echo "  --region-from       Source region to migrate from (eu or me)"
  echo "  --region-to         Target region to migrate to   (eu or me)"
  echo "  --auth-config-name  Name of the KonnectAPIAuthConfiguration resource"
  echo "  --namespace         Kubernetes namespace containing the resources"
  echo "  --help              Show this help message and exit"
  echo ""
  echo -e "${BOLD}Description:${RESET}"
  echo "  Migrate your Dataplanes managed by Kong Operator from one Konnect region to another."
  echo "" 
  echo -e "${BOLD}Example:${RESET}"
  echo "  $0 --region-from eu --region-to me --auth-config-name my-auth-config --namespace kong-system"
  exit "${exit_code}"
}

FROM_REGION=""
TO_REGION=""
KONNECT_AUTH_NAME=""
NAMESPACE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region-from)      FROM_REGION="$2";       shift 2 ;;
    --region-to)        TO_REGION="$2";         shift 2 ;;
    --auth-config-name) KONNECT_AUTH_NAME="$2"; shift 2 ;;
    --namespace)        NAMESPACE="$2";         shift 2 ;;
    --help)             usage 0 ;;
    *) log_error "Unknown flag: $1"; usage ;;
  esac
done

if [[ -z "${FROM_REGION}" || -z "${TO_REGION}" || -z "${KONNECT_AUTH_NAME}" || -z "${NAMESPACE}" ]]; then
  log_error "All flags are required."
  usage
fi

if [[ "$FROM_REGION" == "$TO_REGION" ]]; then
  log_error "from-region and to-region must be different"
  exit 1
fi

if [[ "$FROM_REGION" != "eu" && "$FROM_REGION" != "me" ]]; then
  log_error "from-region must be 'eu' or 'me', got: $FROM_REGION"
  exit 1
fi

if [[ "$TO_REGION" != "eu" && "$TO_REGION" != "me" ]]; then
  log_error "to-region must be 'eu' or 'me', got: $TO_REGION"
  exit 1
fi

AUTH_TO_SERVER_URL="${TO_REGION}.api.konghq.com"
TO_SERVER_URL="https://${TO_REGION}.api.konghq.com"

# ---------------------------------------------------------------------------
# Walk the reference chain from KonnectAPIAuthConfiguration to all dependent
# resources:
# KonnectAPIAuthConfiguration -> KonnectGatewayControlPlane(s)
#   -> KonnectExtension(s) -> DataPlane(s) -> Deployment(s)
# ---------------------------------------------------------------------------

log_info "Resolving resource chain from ${BOLD}KonnectAPIAuthConfiguration/${KONNECT_AUTH_NAME}${RESET} in namespace '${NAMESPACE}'..."

log_resolve "KonnectAPIAuthConfiguration: ${BOLD}${KONNECT_AUTH_NAME}${RESET} (namespace: ${NAMESPACE})"

# KonnectAPIAuthConfiguration -> KonnectGatewayControlPlane(s)
readarray -t CP_NAMES < <(kubectl get konnectgatewaycontrolplane \
  -n "${NAMESPACE}" \
  -o json \
  | jq -r --arg auth "${KONNECT_AUTH_NAME}" \
    '.items[] | select(.spec.konnect.authRef.name == $auth) | .metadata.name')

if [[ ${#CP_NAMES[@]} -eq 0 ]]; then
  log_error "No KonnectGatewayControlPlane resources found referencing '${KONNECT_AUTH_NAME}' in namespace '${NAMESPACE}'"
  exit 1
fi

log_resolve "KonnectGatewayControlPlane(s) referencing ${BOLD}${KONNECT_AUTH_NAME}${RESET}:"
for CP_NAME in "${CP_NAMES[@]}"; do
  log_detail "  - ${CP_NAME}"

  # KonnectGatewayControlPlane -> KonnectExtension(s)
  readarray -t EXT_NAMES < <(kubectl get konnectextension \
    -n "${NAMESPACE}" \
    -o json \
    | jq -r --arg cp "${CP_NAME}" \
      '.items[] | select(.spec.konnect.controlPlane.ref.konnectNamespacedRef.name == $cp) | .metadata.name')

  log_resolve "  KonnectExtension(s) referencing ${BOLD}${CP_NAME}${RESET}:"
  for EXT_NAME in "${EXT_NAMES[@]}"; do
    log_detail "    - ${EXT_NAME}"

    # KonnectExtension -> DataPlane(s)
    readarray -t DP_NAMES < <(kubectl get dataplane \
      -n "${NAMESPACE}" \
      -o json \
      | jq -r --arg ext "${EXT_NAME}" \
        '.items[] | select(.spec.extensions[]? | .name == $ext) | .metadata.name')

    log_resolve "    DataPlane(s) referencing ${BOLD}${EXT_NAME}${RESET}:"
    for DP_NAME in "${DP_NAMES[@]}"; do
      # DataPlane -> Deployment (via OwnerReference UID)
      DP_UID=$(kubectl get dataplane "${DP_NAME}" \
        -n "${NAMESPACE}" \
        -o jsonpath='{.metadata.uid}')
      DEPLOY_NAME=$(kubectl get deployment \
        -n "${NAMESPACE}" \
        -l "gateway-operator.konghq.com/managed-by=dataplane" \
        -o json \
        | jq -r --arg uid "${DP_UID}" \
          '.items[] | select(.metadata.ownerReferences[]? | .uid == $uid) | .metadata.name')
      log_detail "      - DataPlane: ${DP_NAME} -> Deployment: ${DEPLOY_NAME}"
    done
  done
done

echo ""
log_info "Migrating ${BOLD}${FROM_REGION}${RESET} -> ${BOLD}${TO_REGION}${RESET} in namespace '${NAMESPACE}'"
echo ""

# ---------------------------------------------------------------------------
# Patch CRD KonnectAPIAuthConfiguration: remove serverURL immutability rule
# ---------------------------------------------------------------------------

CRD_NAME="konnectapiauthconfigurations.konnect.konghq.com"
CRD_VALIDATIONS_PATH="/spec/versions/0/schema/openAPIV3Schema/properties/spec/properties/serverURL/x-kubernetes-validations"

log_patch "CRD ${CRD_NAME}: saving current serverURL validations..."
ORIGINAL_VALIDATIONS=$(kubectl get crd "${CRD_NAME}" \
  -o json | jq '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.serverURL["x-kubernetes-validations"]')

NEW_VALIDATIONS=$(echo "${ORIGINAL_VALIDATIONS}" | jq '[.[] | select(.message != "Server URL is immutable")]')

log_patch "CRD ${CRD_NAME}: removing serverURL immutability rule..."
while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch crd "${CRD_NAME}" \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"${CRD_VALIDATIONS_PATH}\",\"value\":${NEW_VALIDATIONS}}]")

echo ""

# ---------------------------------------------------------------------------
# Scale down kong-operator deployments (all namespaces)
# ---------------------------------------------------------------------------

log_scale "Scaling down kong-operator deployments in all namespaces..."
while IFS=' ' read -r ns name; do
  log_scale "  deployment/${name} in namespace ${ns} -> ${BOLD}0${RESET}"
  kubectl scale deployment "${name}" -n "${ns}" --replicas=0
done < <(kubectl get deployment \
  -l "app.kubernetes.io/component=ko,app.kubernetes.io/instance=kong-operator" \
  --all-namespaces \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}')

echo ""

# ---------------------------------------------------------------------------
# KonnectAPIAuthConfiguration
# ---------------------------------------------------------------------------

log_patch "konnectapiauthconfiguration/${KONNECT_AUTH_NAME} spec.serverURL"
log_detail "${AUTH_TO_SERVER_URL}"
while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch konnectapiauthconfiguration "${KONNECT_AUTH_NAME}" \
  -n "${NAMESPACE}" \
  --type=merge \
  -p "{\"spec\":{\"serverURL\":\"${AUTH_TO_SERVER_URL}\"}}")

log_patch "konnectapiauthconfiguration/${KONNECT_AUTH_NAME} status.serverURL"
log_detail "${AUTH_TO_SERVER_URL}"
while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch konnectapiauthconfiguration "${KONNECT_AUTH_NAME}" \
  -n "${NAMESPACE}" \
  --type=merge \
  --subresource=status \
  -p "{\"status\":{\"serverURL\":\"https://${AUTH_TO_SERVER_URL}\"}}")

echo ""

# ---------------------------------------------------------------------------
# KonnectGatewayControlPlane(s) — status only
# ---------------------------------------------------------------------------

# Track all deployments patched so we can wait for rollout at the end.
PATCHED_DEPLOYMENTS=()

for CP_NAME in "${CP_NAMES[@]}"; do
  CP_ENDPOINT=$(kubectl get konnectgatewaycontrolplane "${CP_NAME}" \
    -n "${NAMESPACE}" \
    -o jsonpath='{.status.konnectEndpoints.controlPlane}')
  TELEMETRY_ENDPOINT=$(kubectl get konnectgatewaycontrolplane "${CP_NAME}" \
    -n "${NAMESPACE}" \
    -o jsonpath='{.status.konnectEndpoints.telemetry}')

  NEW_CP_ENDPOINT="${CP_ENDPOINT//.${FROM_REGION}./.${TO_REGION}.}"
  NEW_TELEMETRY_ENDPOINT="${TELEMETRY_ENDPOINT//.${FROM_REGION}./.${TO_REGION}.}"

  log_patch "konnectgatewaycontrolplane/${CP_NAME} status"
  log_detail "controlPlane:  ${CP_ENDPOINT} -> ${NEW_CP_ENDPOINT}"
  log_detail "telemetry:     ${TELEMETRY_ENDPOINT} -> ${NEW_TELEMETRY_ENDPOINT}"
  while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch konnectgatewaycontrolplane "${CP_NAME}" \
    -n "${NAMESPACE}" \
    --type=merge \
    --subresource=status \
    -p "{\"status\":{\"serverURL\":\"${TO_SERVER_URL}\",\"konnectEndpoints\":{\"controlPlane\":\"${NEW_CP_ENDPOINT}\",\"telemetry\":\"${NEW_TELEMETRY_ENDPOINT}\"}}}")

  echo ""

  # -------------------------------------------------------------------------
  # KonnectExtension(s) — status only
  # -------------------------------------------------------------------------

  readarray -t EXT_NAMES < <(kubectl get konnectextension \
    -n "${NAMESPACE}" \
    -o json \
    | jq -r --arg cp "${CP_NAME}" \
      '.items[] | select(.spec.konnect.controlPlane.ref.konnectNamespacedRef.name == $cp) | .metadata.name')

  for EXT_NAME in "${EXT_NAMES[@]}"; do
    KE_CP_ENDPOINT=$(kubectl get konnectextension "${EXT_NAME}" \
      -n "${NAMESPACE}" \
      -o jsonpath='{.status.konnect.endpoints.controlPlane}')
    KE_TELEMETRY_ENDPOINT=$(kubectl get konnectextension "${EXT_NAME}" \
      -n "${NAMESPACE}" \
      -o jsonpath='{.status.konnect.endpoints.telemetry}')

    NEW_KE_CP_ENDPOINT="${KE_CP_ENDPOINT//.${FROM_REGION}./.${TO_REGION}.}"
    NEW_KE_TELEMETRY_ENDPOINT="${KE_TELEMETRY_ENDPOINT//.${FROM_REGION}./.${TO_REGION}.}"

    log_patch "konnectextension/${EXT_NAME} status"
    log_detail "controlPlane:  ${KE_CP_ENDPOINT} -> ${NEW_KE_CP_ENDPOINT}"
    log_detail "telemetry:     ${KE_TELEMETRY_ENDPOINT} -> ${NEW_KE_TELEMETRY_ENDPOINT}"
    while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch konnectextension "${EXT_NAME}" \
      -n "${NAMESPACE}" \
      --type=merge \
      --subresource=status \
      -p "{\"status\":{\"konnect\":{\"endpoints\":{\"controlPlane\":\"${NEW_KE_CP_ENDPOINT}\",\"telemetry\":\"${NEW_KE_TELEMETRY_ENDPOINT}\"}}}}")

    echo ""

    # -----------------------------------------------------------------------
    # DataPlane(s) -> Deployment(s) — env vars
    # -----------------------------------------------------------------------

    readarray -t DP_NAMES < <(kubectl get dataplane \
      -n "${NAMESPACE}" \
      -o json \
      | jq -r --arg ext "${EXT_NAME}" \
        '.items[] | select(.spec.extensions[]? | .name == $ext) | .metadata.name')

    for DP_NAME in "${DP_NAMES[@]}"; do
      DP_UID=$(kubectl get dataplane "${DP_NAME}" \
        -n "${NAMESPACE}" \
        -o jsonpath='{.metadata.uid}')
      DEPLOYMENT_NAME=$(kubectl get deployment \
        -n "${NAMESPACE}" \
        -l "gateway-operator.konghq.com/managed-by=dataplane" \
        -o json \
        | jq -r --arg uid "${DP_UID}" \
          '.items[] | select(.metadata.ownerReferences[]? | .uid == $uid) | .metadata.name')

      DP_TELEMETRY_ENDPOINT=$(kubectl get deployment "${DEPLOYMENT_NAME}" \
        -n "${NAMESPACE}" \
        -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="KONG_CLUSTER_TELEMETRY_ENDPOINT")].value}')
      DP_TELEMETRY_SERVER_NAME=$(kubectl get deployment "${DEPLOYMENT_NAME}" \
        -n "${NAMESPACE}" \
        -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="KONG_CLUSTER_TELEMETRY_SERVER_NAME")].value}')
      DP_CLUSTER_SERVER_NAME=$(kubectl get deployment "${DEPLOYMENT_NAME}" \
        -n "${NAMESPACE}" \
        -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="KONG_CLUSTER_SERVER_NAME")].value}')

      NEW_DP_TELEMETRY_ENDPOINT="${DP_TELEMETRY_ENDPOINT//.${FROM_REGION}./.${TO_REGION}.}"
      NEW_DP_TELEMETRY_SERVER_NAME="${DP_TELEMETRY_SERVER_NAME//.${FROM_REGION}./.${TO_REGION}.}"

      log_patch "deployment/${DEPLOYMENT_NAME} env vars"
      log_detail "KONG_CLUSTER_TELEMETRY_ENDPOINT:    ${DP_TELEMETRY_ENDPOINT} -> ${NEW_DP_TELEMETRY_ENDPOINT}"
      log_detail "KONG_CLUSTER_TELEMETRY_SERVER_NAME: ${DP_TELEMETRY_SERVER_NAME} -> ${NEW_DP_TELEMETRY_SERVER_NAME}"

      ENV_ARGS=(
        "KONG_CLUSTER_TELEMETRY_ENDPOINT=${NEW_DP_TELEMETRY_ENDPOINT}"
        "KONG_CLUSTER_TELEMETRY_SERVER_NAME=${NEW_DP_TELEMETRY_SERVER_NAME}"
      )

      if [[ -n "${DP_CLUSTER_SERVER_NAME}" ]]; then
        NEW_DP_CLUSTER_SERVER_NAME="${DP_CLUSTER_SERVER_NAME//.${FROM_REGION}./.${TO_REGION}.}"
        log_detail "KONG_CLUSTER_SERVER_NAME:           ${DP_CLUSTER_SERVER_NAME} -> ${NEW_DP_CLUSTER_SERVER_NAME}"
        ENV_ARGS+=("KONG_CLUSTER_SERVER_NAME=${NEW_DP_CLUSTER_SERVER_NAME}")
      fi

      kubectl set env "deployment/${DEPLOYMENT_NAME}" \
        -n "${NAMESPACE}" \
        "${ENV_ARGS[@]}"

      PATCHED_DEPLOYMENTS+=("${DEPLOYMENT_NAME}")
      echo ""
    done
  done
done

# ---------------------------------------------------------------------------
# Revert CRD KonnectAPIAuthConfiguration: restore serverURL immutability rule
# ---------------------------------------------------------------------------

log_patch "CRD ${CRD_NAME}: restoring serverURL immutability rule..."
RESTORED_VALIDATIONS=$(echo "${ORIGINAL_VALIDATIONS}" \
  | jq '[.[] | select(.message != "Server URL is immutable")] + [{"message": "Server URL is immutable", "rule": "self == oldSelf"}]')
while IFS= read -r line; do log_patch "${line}"; done < <(kubectl patch crd "${CRD_NAME}" \
  --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"${CRD_VALIDATIONS_PATH}\",\"value\":${RESTORED_VALIDATIONS}}]")

# ---------------------------------------------------------------------------
# Scale kong-operator deployments back up (all namespaces)
# ---------------------------------------------------------------------------

echo ""
log_scale "Scaling kong-operator deployments back to 1 in all namespaces..."
while IFS=' ' read -r ns name; do
  log_scale "  deployment/${name} in namespace ${ns} -> ${BOLD}1${RESET}"
  kubectl scale deployment "${name}" -n "${ns}" --replicas=1
done < <(kubectl get deployment \
  -l "app.kubernetes.io/component=ko,app.kubernetes.io/instance=kong-operator" \
  --all-namespaces \
  -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}')

echo ""
for DEPLOYMENT_NAME in "${PATCHED_DEPLOYMENTS[@]}"; do
  log_wait "Waiting for deployment/${DEPLOYMENT_NAME} to roll out..."
  while IFS= read -r line; do
    if [[ "${line}" == *"successfully rolled out"* ]]; then
      log_ok "${line}"
    else
      log_wait "${line}"
    fi
  done < <(kubectl rollout status deployment "${DEPLOYMENT_NAME}" -n "${NAMESPACE}")
done

echo ""
log_ok "${BOLD}Migration complete: ${FROM_REGION} -> ${TO_REGION}${RESET}"
