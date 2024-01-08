package controlplane

import k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// ConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the ControlPlane have been provisioned
	// successfully.
	ConditionTypeProvisioned k8sutils.ConditionType = "Provisioned"
)

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

// ConditionReason are the condition reasons for ControlPlane status conditions.
type ConditionReason string

const (
	// ConditionReasonPodsNotReady is a reason which indicates why a ControlPlane
	// has not yet reached a fully Provisioned status.
	ConditionReasonPodsNotReady k8sutils.ConditionReason = "PodsNotReady"

	// ConditionReasonPodsReady is a reason which indicates how a ControlPlane
	// reached fully Provisioned status.
	ConditionReasonPodsReady k8sutils.ConditionReason = "PodsReady"

	// ControlPlaneConditionsReasonNoDataplane is a reason which indicates that no DataPlane
	// has been provisioned.
	ConditionReasonNoDataplane k8sutils.ConditionReason = "NoDataplane"
)
