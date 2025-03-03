package dataplane

import "github.com/kong/kubernetes-configuration/api/common/consts"

// -----------------------------------------------------------------------------
// DataPlane - Status Condition Reasons
// -----------------------------------------------------------------------------

const (
	// DataPlaneConditionValidationFailed is a reason which indicates validation of
	// a dataplane is failed.
	DataPlaneConditionValidationFailed consts.ConditionReason = "ValidationFailed"

	// DataPlaneConditionReferencedResourcesNotAvailable is a reason which indicates
	// that the referenced resources in DataPlane configuration (e.g. KongPluginInstallation)
	// are not available.
	DataPlaneConditionReferencedResourcesNotAvailable consts.ConditionReason = "ReferencedResourcesNotAvailable"
)

// -----------------------------------------------------------------------------
// DataPlane - Ready Condition Constants
// -----------------------------------------------------------------------------

const (
	// ReadyType indicates if the resource has all the dependent conditions Ready
	ReadyType consts.ConditionType = "Ready"

	// DependenciesNotReadyReason is a generic reason describing that the other Conditions are not true
	DependenciesNotReadyReason consts.ConditionReason = "DependenciesNotReady"
	// ResourceReadyReason indicates the resource is ready
	ResourceReadyReason consts.ConditionReason = consts.ConditionReason("Ready")
	// WaitingToBecomeReadyReason generic message for dependent resources waiting to be ready
	WaitingToBecomeReadyReason consts.ConditionReason = "WaitingToBecomeReady"
	// ResourceCreatedOrUpdatedReason generic message for missing or outdated resources
	ResourceCreatedOrUpdatedReason consts.ConditionReason = "ResourceCreatedOrUpdated"
	// UnableToProvisionReason generic message for unexpected errors
	UnableToProvisionReason consts.ConditionReason = "UnableToProvision"

	// DependenciesNotReadyMessage indicates the other conditions are not yet ready
	DependenciesNotReadyMessage = "There are other conditions that are not yet ready"
	// WaitingToBecomeReadyMessage indicates the target resource is not ready
	WaitingToBecomeReadyMessage = "Waiting for the resource to become ready"
	// ResourceCreatedMessage indicates a missing resource was provisioned
	ResourceCreatedMessage = "Resource has been created"
	// ResourceUpdatedMessage indicates a resource was updated
	ResourceUpdatedMessage = "Resource has been updated"
)
