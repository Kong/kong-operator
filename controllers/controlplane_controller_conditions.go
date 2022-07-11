package controllers

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Types
// -----------------------------------------------------------------------------

// ControlPlaneConditionType are condition types for ControlPlane status conditions.
type ControlPlaneConditionType string

const (
	// ControlPlaneConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the ControlPlane have been provisioned
	// successfully.
	ControlPlaneConditionTypeProvisioned ControlPlaneConditionType = "Provisioned"
)

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

// ControlPlaneConditionReason are the condition reasons for ControlPlane status conditions.
type ControlPlaneConditionReason string

const (
	// ControlPlaneConditionReasonPodsNotReady is a reason which indicates why a ControlPlane
	// has not yet reached a fully Provisioned status.
	ControlPlaneConditionReasonPodsNotReady = "PodsNotReady"

	// ControlPlaneConditionReasonPodsReady is a reason which indicates how a ControlPlane
	// reached fully Provisioned status.
	ControlPlaneConditionReasonPodsReady = "PodsReady"

	// ControlPlaneConditionsReasonNoDataplane is a reason which indicates that no DataPlane
	// has been provisioned.
	ControlPlaneConditionReasonNoDataplane = "NoDataplane"
)
