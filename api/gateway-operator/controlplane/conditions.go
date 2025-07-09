package controlplane

import "github.com/kong/kubernetes-configuration/v2/api/common/consts"

// -----------------------------------------------------------------------------
// ControlPlane - Status Condition Types
// -----------------------------------------------------------------------------

const (
	// ConditionTypeProvisioned is a condition type indicating whether or
	// not all Deployments (or Daemonsets) for the ControlPlane have been provisioned
	// successfully.
	ConditionTypeProvisioned consts.ConditionType = "Provisioned"

	// ConditionTypeWatchNamespaceGrantValid is a condition type used to
	// indicate whether or not the ControlPlane has been granted permission to
	// watch resources in the requested namespaces.
	ConditionTypeWatchNamespaceGrantValid consts.ConditionType = "WatchNamespaceGrantValid"
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

	// ConditionReasonWatchNamespaceGrantInvalid is a reason which indicates that
	// WatchNamespaceGrants are invalid or missing for the ControlPlane to be able
	// to watch resources in requested namespaces.
	ConditionReasonWatchNamespaceGrantInvalid consts.ConditionReason = "WatchNamespaceGrantInvalid"

	// ConditionReasonWatchNamespaceGrantValid is a reason which indicates that
	// WatchNamespaceGrants are valid for the ControlPlane to be able to watch
	// resources in requested namespaces.
	ConditionReasonWatchNamespaceGrantValid consts.ConditionReason = "WatchNamespaceGrantValid"
)
