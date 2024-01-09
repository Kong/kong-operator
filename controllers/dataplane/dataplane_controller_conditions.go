package dataplane

import k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// DataPlaneConditionValidationFailed is a reason which indicates validation of
	// a dataplane is failed.
	DataPlaneConditionValidationFailed k8sutils.ConditionReason = "ValidationFailed"
)
