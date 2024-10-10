package consts

// ConditionType literal that defines the different types of condition
type ConditionType string

// CoditionReason literal to enumerate a specific condition reason
type ConditionReason string

// -----------------------------------------------------------------------------
// DataPlane - Ready Condition Constants
// -----------------------------------------------------------------------------

const (
	// ReadyType indicates if the resource has all the dependent conditions Ready
	ReadyType ConditionType = "Ready"

	// DependenciesNotReadyReason is a generic reason describing that the other Conditions are not true
	DependenciesNotReadyReason ConditionReason = "DependenciesNotReady"
	// ResourceReadyReason indicates the resource is ready
	ResourceReadyReason ConditionReason = ConditionReason("Ready")
	// WaitingToBecomeReadyReason generic message for dependent resources waiting to be ready
	WaitingToBecomeReadyReason ConditionReason = "WaitingToBecomeReady"
	// ResourceCreatedOrUpdatedReason generic message for missing or outdated resources
	ResourceCreatedOrUpdatedReason ConditionReason = "ResourceCreatedOrUpdated"
	// UnableToProvisionReason generic message for unexpected errors
	UnableToProvisionReason ConditionReason = "UnableToProvision"

	// DependenciesNotReadyMessage indicates the other conditions are not yet ready
	DependenciesNotReadyMessage = "There are other conditions that are not yet ready"
	// WaitingToBecomeReadyMessage indicates the target resource is not ready
	WaitingToBecomeReadyMessage = "Waiting for the resource to become ready"
	// ResourceCreatedMessage indicates a missing resource was provisioned
	ResourceCreatedMessage = "Resource has been created"
	// ResourceUpdatedMessage indicates a resource was updated
	ResourceUpdatedMessage = "Resource has been updated"
)

// -----------------------------------------------------------------------------
// DataPlane - ResolvedRefs Condition Constants
// -----------------------------------------------------------------------------

const (
	// ResolvedRefsType indicates if the resource has all the dependent references resolved
	ResolvedRefsType ConditionType = "ResolvedRefs"

	// ResolvedRefsReason is a generic reason describing that the references are resolved. It must be used when the ResolvedRefs condition is set to True.
	ResolvedRefsReason ConditionReason = "ResolvedRefs"
	// RefNotPermittedReason is a generic reason describing that the reference is not permitted. It must be used when the ResolvedRefs condition is set to False.
	RefNotPermittedReason ConditionReason = "RefNotPermitted"
	// InvalidExtensionRefReason is a generic reason describing that the extension reference is invalid. It must be used when the ResolvedRefs condition is set to False.
	InvalidExtensionRefReason ConditionReason = "InvalidExtension"
	// InvalidSecretRefReason is a generic reason describing that the secret reference is invalid. It must be used when the ResolvedRefs condition is set to False.
	InvalidSecretRefReason ConditionReason = "InvalidSecret"
)
