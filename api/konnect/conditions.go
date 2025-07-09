package konnect

import "github.com/kong/kubernetes-configuration/v2/api/common/consts"

// -----------------------------------------------------------------------------
// DataPlane - Extensions conditions Constants
// -----------------------------------------------------------------------------

const (
	// KonnectExtensionAppliedType indicates that the KonnectExtension has been applied
	KonnectExtensionAppliedType consts.ConditionType = "KonnectExtensionApplied"

	// KonnectExtensionAppliedReason is a reason describing that the Konnect extension has been applied. It must be used when the KonnectExtensionApplied condition is set to True.
	KonnectExtensionAppliedReason consts.ConditionReason = "KonnectExtensionApplied"
	// RefNotPermittedReason is a reason describing that the cross-namespace reference is not permitted. It must be used when the KonnectExtensionApplied condition is set to False.
	RefNotPermittedReason consts.ConditionReason = "RefNotPermitted"
	// InvalidExtensionRefReason is a reason describing that the extension reference is invalid. It must be used when the KonnectExtensionApplied condition is set to False.
	InvalidExtensionRefReason consts.ConditionReason = "InvalidExtension"
	// InvalidSecretRefReason is a reason describing that the secret reference in the KonnectExtension is invalid. It must be used when the KonnectExtensionApplied condition is set to False.
	InvalidSecretRefReason consts.ConditionReason = "InvalidSecret"
	// KonnectExtensionNotReadyReason is a reason describing that the Konnect extension is not ready. It must be used when the KonnectExtensionApplied condition is set to False.
	KonnectExtensionNotReadyReason consts.ConditionReason = "KonnectExtensionNotReady"
)

const (
	// AcceptedExtensionsType inditicates if the resource has all the dependent extensions accepted
	AcceptedExtensionsType consts.ConditionType = "AcceptedExtensions"

	// AcceptedExtensionsReason is a reason describing that the extensions are accepted. It must be used when the AcceptedExtensions condition is set to True.
	AcceptedExtensionsReason consts.ConditionReason = "AcceptedExtensions"
	// NotSupportedExtensionsReason is a reason describing that the extensions are not supported. It must be used when the AcceptedExtensions condition is set to False.
	NotSupportedExtensionsReason consts.ConditionReason = "NotSupportedExtensions"
)

// -----------------------------------------------------------------------------
// Konnect entities - Programmed Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectEntitiesFailedToCreateReason is the reason assigned to Konnect entities that failed to get created.
	// It must be used when Programmed condition is set to False.
	KonnectEntitiesFailedToCreateReason consts.ConditionReason = "FailedToCreate"
	// KonnectEntitiesFailedToUpdateReason is the reason assigned to Konnect entities that failed to get updated.
	// It must be used when Programmed condition is set to False.
	KonnectEntitiesFailedToUpdateReason consts.ConditionReason = "FailedToUpdate"
	// FailedToAttachConsumerToConsumerGroupReason is the reason assigned to KonnConsumers when failed to attach it to any consumer group.
	// It must be used when Programmed condition is set to False.
	FailedToAttachConsumerToConsumerGroupReason consts.ConditionReason = "FailedToAttachConsumerToConsumerGroup"
)
