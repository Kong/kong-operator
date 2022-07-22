package controllers

import k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Types
// -----------------------------------------------------------------------------

// DataPlaneConditionType are condition types for DataPlane status conditions.
type DataPlaneConditionType string

const (
	// DataPlaneConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the DataPlane have been provisioned
	// successfully.
	DataPlaneConditionTypeProvisioned k8sutils.ConditionType = "Provisioned"
)

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

// DataPlaneConditionReason are the condition reasons for DataPlane status conditions.
type DataPlaneConditionReason string

const (
	// DataPlaneConditionReasonPodsNotReady is a reason which indicates why a DataPlane
	// has not yet reached a fully Provisioned status.
	DataPlaneConditionReasonPodsNotReady k8sutils.ConditionReason = "PodsNotReady"

	// DataPlaneConditionReasonPodsReady is a reason which indicates how a DataPlane
	// reached fully Provisioned status.
	DataPlaneConditionReasonPodsReady k8sutils.ConditionReason = "PodsReady"

	// DataPlaneConditionValidationFailed is a reason which indicates validation of
	// a dataplane is failed.
	DataPlaneConditionValidationFailed k8sutils.ConditionReason = "ValidationFailed"
)
