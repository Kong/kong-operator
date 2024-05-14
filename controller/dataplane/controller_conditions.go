package dataplane

import "github.com/kong/gateway-operator/pkg/consts"

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// DataPlaneConditionValidationFailed is a reason which indicates validation of
	// a dataplane is failed.
	DataPlaneConditionValidationFailed consts.ConditionReason = "ValidationFailed"
)
