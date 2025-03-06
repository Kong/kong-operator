package dataplane

import (
	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
)

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// DataPlaneConditionValidationFailed is a reason which indicates validation of
	// a dataplane is failed.
	DataPlaneConditionValidationFailed kcfgconsts.ConditionReason = "ValidationFailed"

	// DataPlaneConditionReferencedResourcesNotAvailable is a reason which indicates
	// that the referenced resources in DataPlane configuration (e.g. KongPluginInstallation)
	// are not available.
	DataPlaneConditionReferencedResourcesNotAvailable kcfgconsts.ConditionReason = "ReferencedResourcesNotAvailable"
)
