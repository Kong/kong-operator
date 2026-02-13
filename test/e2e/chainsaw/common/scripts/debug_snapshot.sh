#!/usr/bin/env bash

# Debug snapshot script for capturing cluster state when tests fail.
# This script captures all relevant resources, events, and logs for debugging.

set -o errexit
set -o nounset
set -o pipefail

# Variables (from environment):
#   TEST_NAME: Name of the test (used for output directory naming).
#   NAMESPACE: The test namespace to capture state from.
#   ADDITIONAL_NAMESPACES: Comma-separated list of additional namespaces (optional).
#   OUTPUT_DIR: Base directory for debug output (default: /tmp/chainsaw).
#   OPERATOR_NAMESPACE: Namespace where the operator runs (default: kong-system).

TEST_NAME="${TEST_NAME:-unknown-test}"
NAMESPACE="${NAMESPACE:-default}"
ADDITIONAL_NAMESPACES="${ADDITIONAL_NAMESPACES:-}"
OUTPUT_DIR="${OUTPUT_DIR:-/tmp/chainsaw}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-kong-system}"

# Build list of all namespaces to capture
ALL_NAMESPACES="${NAMESPACE}"
if [ -n "${ADDITIONAL_NAMESPACES}" ]; then
  # Convert comma-separated list to space-separated for iteration
  ADDITIONAL_NS_LIST=$(echo "${ADDITIONAL_NAMESPACES}" | tr ',' ' ')
  ALL_NAMESPACES="${NAMESPACE} ${ADDITIONAL_NS_LIST}"
fi

# Create output directory structure
SNAPSHOT_DIR="${OUTPUT_DIR}/${TEST_NAME}"
mkdir -p "${SNAPSHOT_DIR}"

echo "=== Capturing debug snapshot for test: ${TEST_NAME} ==="
echo "Test namespace(s): ${ALL_NAMESPACES}"
echo "Operator namespace: ${OPERATOR_NAMESPACE}"
echo "Output directory: ${SNAPSHOT_DIR}"
echo ""

# Function to safely run kubectl commands and capture output
safe_kubectl() {
  local output_file=$1
  shift
  echo "Running: kubectl $*" >> "${output_file}"
  echo "---" >> "${output_file}"
  if kubectl "$@" >> "${output_file}" 2>&1; then
    echo "" >> "${output_file}"
    return 0
  else
    echo "Error running command (exit code: $?)" >> "${output_file}"
    echo "" >> "${output_file}"
    return 0  # Don't fail the script if a command fails
  fi
}

# 1. Capture all resources in test namespaces (YAML format)
echo "Capturing all resources in namespace(s)..."
RESOURCES_FILE="${SNAPSHOT_DIR}/resources.yaml"
{
  echo "# All resources in test namespaces"
  echo "# Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "---"
} > "${RESOURCES_FILE}"

# Function to capture a group of resources with a header
capture_resource_group() {
  local output_file=$1
  local namespace=$2
  local group_name=$3
  local resource_types=$4

  {
    echo ""
    echo "# ------------------------------------------"
    echo "# ${group_name}"
    echo "# ------------------------------------------"
    echo ""
  } >> "${output_file}"

  safe_kubectl "${output_file}" get "${resource_types}" -n "${namespace}" -o yaml
}

# Capture resources from all namespaces, organized by type for easy navigation
for ns in ${ALL_NAMESPACES}; do
  echo "Capturing resources from namespace: ${ns}..."
  {
    echo ""
    echo "# =========================================="
    echo "# Namespace: ${ns}"
    echo "# =========================================="
    echo ""
  } >> "${RESOURCES_FILE}"

  # Gateway API resources (using category for all Gateway API resources)
  capture_resource_group "${RESOURCES_FILE}" "${ns}" "Gateway API Resources" \
    "gateway-api"

  # Kong Configuration resources
  capture_resource_group "${RESOURCES_FILE}" "${ns}" "Kong Configuration Resources" \
    "kongcertificates.configuration.konghq.com,kongsnis.configuration.konghq.com,kongroutes.configuration.konghq.com,kongservices.configuration.konghq.com,kongupstreams.configuration.konghq.com,kongtargets.configuration.konghq.com,kongreferencegrants.configuration.konghq.com,kongplugins.configuration.konghq.com,kongclusterplugins.configuration.konghq.com,kongconsumers.configuration.konghq.com,kongconsumergroups.configuration.konghq.com"

  # Konnect resources (excluding KonnectAPIAuthConfiguration - captured separately with redaction)
  capture_resource_group "${RESOURCES_FILE}" "${ns}" "Konnect Resources" \
    "konnectgatewaycontrolplanes.konnect.konghq.com,konnectextensions.konnect.konghq.com"

  # KonnectAPIAuthConfiguration with token redaction
  {
    echo ""
    echo "# ------------------------------------------"
    echo "# Konnect API Auth Configurations (tokens redacted)"
    echo "# ------------------------------------------"
    echo ""
  } >> "${RESOURCES_FILE}"

  # Capture and redact tokens
  if kubectl get konnectapiauthconfigurations.konnect.konghq.com -n "${ns}" -o yaml 2>/dev/null | \
     sed 's/\(token: \).*/\1"[REDACTED]"/g' >> "${RESOURCES_FILE}" 2>&1; then
    echo "" >> "${RESOURCES_FILE}"
  else
    echo "# No KonnectAPIAuthConfigurations found or error occurred" >> "${RESOURCES_FILE}"
    echo "" >> "${RESOURCES_FILE}"
  fi

  # Gateway Operator resources
  capture_resource_group "${RESOURCES_FILE}" "${ns}" "Gateway Operator Resources" \
    "gatewayconfigurations.gateway-operator.konghq.com,controlplanes.gateway-operator.konghq.com,dataplanes.gateway-operator.konghq.com,aigateways.gateway-operator.konghq.com,dataplanemetricsextensions.gateway-operator.konghq.com"

  # Core Kubernetes resources
  capture_resource_group "${RESOURCES_FILE}" "${ns}" "Core Kubernetes Resources" \
    "pods,services,deployments.apps,replicasets.apps,statefulsets.apps,configmaps,serviceaccounts,roles.rbac.authorization.k8s.io,rolebindings.rbac.authorization.k8s.io"
done

# 2. Capture detailed descriptions of all resources
echo "Capturing resource descriptions..."
DESCRIBED_FILE="${SNAPSHOT_DIR}/resources-described.txt"
{
  echo "=== Detailed Resource Descriptions ==="
  echo "Namespaces: ${ALL_NAMESPACES}"
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
} > "${DESCRIBED_FILE}"

# Loop through all namespaces
for ns in ${ALL_NAMESPACES}; do
  echo "Capturing descriptions from namespace: ${ns}..."
  {
    echo ""
    echo "######################################################"
    echo "# Namespace: ${ns}"
    echo "######################################################"
    echo ""
  } >> "${DESCRIBED_FILE}"

  safe_kubectl "${DESCRIBED_FILE}" describe all -n "${ns}"

  # Also describe Gateway API and Kong resources specifically
  {
    echo ""
    echo "=== Gateway API Resources in ${ns} ==="
    echo ""
  } >> "${DESCRIBED_FILE}"

  safe_kubectl "${DESCRIBED_FILE}" describe gateways,gatewayclasses,httproutes,referencegrants -n "${ns}"

  {
    echo ""
    echo "=== Kong Configuration Resources in ${ns} ==="
    echo ""
  } >> "${DESCRIBED_FILE}"

  safe_kubectl "${DESCRIBED_FILE}" describe kongcertificates,kongsnis,kongroutes,kongservices,kongupstreams,kongtargets,kongreferencegrants,kongplugins -n "${ns}"

  {
    echo ""
    echo "=== Konnect Resources in ${ns} ==="
    echo ""
  } >> "${DESCRIBED_FILE}"

  safe_kubectl "${DESCRIBED_FILE}" describe konnectgatewaycontrolplanes,konnectextensions -n "${ns}"

  # Describe KonnectAPIAuthConfigurations with token redaction
  {
    echo ""
    echo "=== KonnectAPIAuthConfigurations (tokens redacted) in ${ns} ==="
    echo ""
  } >> "${DESCRIBED_FILE}"

  if kubectl describe konnectapiauthconfigurations -n "${ns}" 2>/dev/null | \
     sed 's/\(Token:\s*\).*/\1[REDACTED]/g' >> "${DESCRIBED_FILE}" 2>&1; then
    echo "" >> "${DESCRIBED_FILE}"
  else
    echo "No KonnectAPIAuthConfigurations found or error occurred" >> "${DESCRIBED_FILE}"
    echo "" >> "${DESCRIBED_FILE}"
  fi

  {
    echo ""
    echo "=== GatewayConfiguration Resources in ${ns} ==="
    echo ""
  } >> "${DESCRIBED_FILE}"

  safe_kubectl "${DESCRIBED_FILE}" describe gatewayconfigurations -n "${ns}"
done

# 3. Capture events in test namespaces
echo "Capturing events in test namespace(s)..."
EVENTS_FILE="${SNAPSHOT_DIR}/events.txt"
{
  echo "=== Events in test namespaces ==="
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
} > "${EVENTS_FILE}"

for ns in ${ALL_NAMESPACES}; do
  echo "Capturing events from namespace: ${ns}..."
  {
    echo ""
    echo "######################################################"
    echo "# Events in namespace: ${ns}"
    echo "######################################################"
    echo ""
  } >> "${EVENTS_FILE}"

  safe_kubectl "${EVENTS_FILE}" get events -n "${ns}" --sort-by='.lastTimestamp'
done

# 4. Capture events in operator namespace
echo "Capturing events in operator namespace ${OPERATOR_NAMESPACE}..."
OPERATOR_EVENTS_FILE="${SNAPSHOT_DIR}/operator-events.txt"
{
  echo "=== Events in operator namespace: ${OPERATOR_NAMESPACE} ==="
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
} > "${OPERATOR_EVENTS_FILE}"

safe_kubectl "${OPERATOR_EVENTS_FILE}" get events -n "${OPERATOR_NAMESPACE}" --sort-by='.lastTimestamp'

# 5. Capture operator logs
echo "Capturing operator logs from namespace ${OPERATOR_NAMESPACE}..."
OPERATOR_LOGS_FILE="${SNAPSHOT_DIR}/operator-logs.txt"
{
  echo "=== Kong Operator Logs ==="
  echo "Namespace: ${OPERATOR_NAMESPACE}"
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
} > "${OPERATOR_LOGS_FILE}"

# Get all operator pods
OPERATOR_PODS=$(kubectl get pods -n "${OPERATOR_NAMESPACE}" -l app.kubernetes.io/name=kong-operator -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")

if [ -n "${OPERATOR_PODS}" ]; then
  for pod in ${OPERATOR_PODS}; do
    echo "=== Logs from pod: ${pod} ===" >> "${OPERATOR_LOGS_FILE}"
    echo "" >> "${OPERATOR_LOGS_FILE}"

    # Get logs with timestamps
    if kubectl logs "${pod}" -n "${OPERATOR_NAMESPACE}" --all-containers --timestamps >> "${OPERATOR_LOGS_FILE}" 2>&1; then
      echo "" >> "${OPERATOR_LOGS_FILE}"
    else
      echo "Failed to get logs from pod ${pod}" >> "${OPERATOR_LOGS_FILE}"
      echo "" >> "${OPERATOR_LOGS_FILE}"
    fi

    # Also get previous logs if container restarted
    echo "=== Previous logs from pod: ${pod} (if any) ===" >> "${OPERATOR_LOGS_FILE}"
    if kubectl logs "${pod}" -n "${OPERATOR_NAMESPACE}" --all-containers --timestamps --previous >> "${OPERATOR_LOGS_FILE}" 2>&1; then
      echo "" >> "${OPERATOR_LOGS_FILE}"
    else
      echo "No previous logs or failed to get previous logs" >> "${OPERATOR_LOGS_FILE}"
      echo "" >> "${OPERATOR_LOGS_FILE}"
    fi
  done
else
  echo "No operator pods found in namespace ${OPERATOR_NAMESPACE}" >> "${OPERATOR_LOGS_FILE}"
fi

# 6. Capture operator pod status
echo "Capturing operator pod status..."
OPERATOR_STATUS_FILE="${SNAPSHOT_DIR}/operator-pods-status.txt"
{
  echo "=== Kong Operator Pods Status ==="
  echo "Namespace: ${OPERATOR_NAMESPACE}"
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
} > "${OPERATOR_STATUS_FILE}"

safe_kubectl "${OPERATOR_STATUS_FILE}" get pods -n "${OPERATOR_NAMESPACE}" -l app.kubernetes.io/name=kong-operator -o wide
safe_kubectl "${OPERATOR_STATUS_FILE}" describe pods -n "${OPERATOR_NAMESPACE}" -l app.kubernetes.io/name=kong-operator

# 7. Create summary file
echo "Creating summary..."
SUMMARY_FILE="${SNAPSHOT_DIR}/summary.txt"
{
  echo "=== Debug Snapshot Summary ==="
  echo "Test Name: ${TEST_NAME}"
  echo "Test Namespaces: ${ALL_NAMESPACES}"
  echo "Operator Namespace: ${OPERATOR_NAMESPACE}"
  echo "Captured at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo ""
  echo "Files generated:"
  echo "  - resources.yaml: All resources in test namespaces (YAML format)"
  echo "  - resources-described.txt: Detailed descriptions of all resources"
  echo "  - events.txt: Events in test namespaces"
  echo "  - operator-events.txt: Events in operator namespace"
  echo "  - operator-logs.txt: Kong operator logs (current and previous)"
  echo "  - operator-pods-status.txt: Operator pod status and descriptions"
  echo ""
} > "${SUMMARY_FILE}"

# Count resources by namespace
for ns in ${ALL_NAMESPACES}; do
  echo "Resource counts in namespace ${ns}:" >> "${SUMMARY_FILE}"

  # Count resources by type
  kubectl api-resources --verbs=list --namespaced -o name | while read -r resource; do
    count=$(kubectl get "${resource}" -n "${ns}" --ignore-not-found 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
    if [ "${count}" != "0" ]; then
      echo "  - ${resource}: ${count}" >> "${SUMMARY_FILE}"
    fi
  done

  echo "" >> "${SUMMARY_FILE}"
done

echo "Operator pod count: $(kubectl get pods -n "${OPERATOR_NAMESPACE}" -l app.kubernetes.io/name=kong-operator --no-headers 2>/dev/null | wc -l | tr -d ' ')" >> "${SUMMARY_FILE}"

echo ""
echo "=== Debug snapshot complete ==="
echo "Output saved to: ${SNAPSHOT_DIR}"
echo ""
echo "Summary:"
cat "${SUMMARY_FILE}"
