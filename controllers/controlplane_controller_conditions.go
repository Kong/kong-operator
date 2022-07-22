package controllers

import k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// ControlPlaneConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the ControlPlane have been provisioned
	// successfully.
	ControlPlaneConditionTypeProvisioned k8sutils.ConditionType = "Provisioned"
)

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

// ControlPlaneConditionReason are the condition reasons for ControlPlane status conditions.
type ControlPlaneConditionReason string

const (
	// ControlPlaneConditionReasonPodsNotReady is a reason which indicates why a ControlPlane
	// has not yet reached a fully Provisioned status.
	ControlPlaneConditionReasonPodsNotReady k8sutils.ConditionReason = "PodsNotReady"

	// ControlPlaneConditionReasonPodsReady is a reason which indicates how a ControlPlane
	// reached fully Provisioned status.
	ControlPlaneConditionReasonPodsReady k8sutils.ConditionReason = "PodsReady"

	// ControlPlaneConditionsReasonNoDataplane is a reason which indicates that no DataPlane
	// has been provisioned.
	ControlPlaneConditionReasonNoDataplane k8sutils.ConditionReason = "NoDataplane"
)
