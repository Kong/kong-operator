package controlplane

import "github.com/kong/kubernetes-configuration/api/common/consts"

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// ConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the ControlPlane have been provisioned
	// successfully.
	ConditionTypeProvisioned consts.ConditionType = "Provisioned"
)

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

// ConditionReason are the condition reasons for ControlPlane status conditions.
type ConditionReason string

const (
	// ConditionReasonPodsNotReady is a reason which indicates why a ControlPlane
	// has not yet reached a fully Provisioned status.
	ConditionReasonPodsNotReady consts.ConditionReason = "PodsNotReady"

	// ConditionReasonPodsReady is a reason which indicates how a ControlPlane
	// reached fully Provisioned status.
	ConditionReasonPodsReady consts.ConditionReason = "PodsReady"

	// ConditionReasonNoDataPlane is a reason which indicates that no DataPlane
	// has been provisioned.
	ConditionReasonNoDataPlane consts.ConditionReason = "NoDataPlane"

	// ConditionReasonMissingReferenceGrant is a reason which indicates that
	// ReferenceGrants are missing for the ControlPlane to be able to watch
	// resources in requested namespaces.
	ConditionReasonMissingReferenceGrant consts.ConditionReason = "MissingReferenceGrants"
)
