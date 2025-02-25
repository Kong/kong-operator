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
// DataPlane - Extensions conditions Constants
// -----------------------------------------------------------------------------

const (
	// KonnectExtensionAppliedType indicates that the KonnectExtension has been applied
	KonnectExtensionAppliedType ConditionType = "KonnectExtensionApplied"

	// KonnectExtensionAppliedReason is a reason describing that the Konnect extension has been applied. It must be used when the KonnectExtensionApplied condition is set to True.
	KonnectExtensionAppliedReason ConditionReason = "KonnectExtensionApplied"
	// RefNotPermittedReason is a reason describing that the cross-namespace reference is not permitted. It must be used when the KonnectExtensionApplied condition is set to False.
	RefNotPermittedReason ConditionReason = "RefNotPermitted"
	// InvalidExtensionRefReason is a reason describing that the extension reference is invalid. It must be used when the KonnectExtensionApplied condition is set to False.
	InvalidExtensionRefReason ConditionReason = "InvalidExtension"
	// InvalidSecretRefReason is a reason describing that the secret reference in the KonnectExtension is invalid. It must be used when the KonnectExtensionApplied condition is set to False.
	InvalidSecretRefReason ConditionReason = "InvalidSecret"
	// KonnectExtensionNotReadyReason is a reason describing that the Konnect extension is not ready. It must be used when the KonnectExtensionApplied condition is set to False.
	KonnectExtensionNotReadyReason ConditionReason = "KonnectExtensionNotReady"
)

const (
	// AcceptedExtensionsType inditicates if the resource has all the dependent extensions accepted
	AcceptedExtensionsType ConditionType = "AcceptedExtensions"

	// AcceptedExtensionsReason is a reason describing that the extensions are accepted. It must be used when the AcceptedExtensions condition is set to True.
	AcceptedExtensionsReason ConditionReason = "AcceptedExtensions"
	// NotSupportedExtensionsReason is a reason describing that the extensions are not supported. It must be used when the AcceptedExtensions condition is set to False.
	NotSupportedExtensionsReason ConditionReason = "NotSupportedExtensions"
)

// -----------------------------------------------------------------------------
// Konnect entities - Programmed Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectEntitiesFailedToCreateReason is the reason assigned to Konnect entities that failed to get created.
	// It must be used when Programmed condition is set to False.
	KonnectEntitiesFailedToCreateReason ConditionReason = "FailedToCreate"
	// KonnectEntitiesFailedToUpdateReason is the reason assigned to Konnect entities that failed to get updated.
	// It must be used when Programmed condition is set to False.
	KonnectEntitiesFailedToUpdateReason ConditionReason = "FailedToUpdate"
	// FailedToAttachConsumerToConsumerGroupReason is the reason assigned to KonnConsumers when failed to attach it to any consumer group.
	// It must be used when Programmed condition is set to False.
	FailedToAttachConsumerToConsumerGroupReason ConditionReason = "FailedToAttachConsumerToConsumerGroup"
)
