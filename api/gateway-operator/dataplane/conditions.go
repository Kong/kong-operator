package dataplane

import "github.com/kong/kubernetes-configuration/v2/api/common/consts"

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

// -----------------------------------------------------------------------------
// DataPlane - BlueGreen Condition Constants
// -----------------------------------------------------------------------------

const (
	// DataPlaneConditionTypeRolledOut is a condition type indicating whether or
	// not, DataPlane's rollout has been successful or not.
	DataPlaneConditionTypeRolledOut consts.ConditionType = "RolledOut"
)

const (
	// DataPlaneConditionReasonRolloutAwaitingPromotion is a reason which indicates a DataPlane
	// preview has been deployed successfully and is awaiting promotion.
	// If this Reason is present and no automated rollout is disabled, user can
	// use the preview services and deployment to inspect the state of those
	// make a judgement call if the promotion should happen.
	DataPlaneConditionReasonRolloutAwaitingPromotion consts.ConditionReason = "AwaitingPromotion"

	// DataPlaneConditionReasonRolloutFailed is a reason which indicates a DataPlane
	// has failed to roll out. This may be caused for example by a Deployment or
	// a Service failing to get created during a rollout.
	DataPlaneConditionReasonRolloutFailed consts.ConditionReason = "Failed"

	// DataPlaneConditionReasonRolloutProgressing is a reason which indicates a DataPlane's
	// new version is being rolled out.
	DataPlaneConditionReasonRolloutProgressing consts.ConditionReason = "Progressing"

	// DataPlaneConditionReasonRolloutWaitingForChange is a reason which indicates a DataPlane
	// is waiting for a change to trigger new version to be made available before promotion.
	DataPlaneConditionReasonRolloutWaitingForChange consts.ConditionReason = "WaitingForChange"

	// DataPlaneConditionReasonRolloutPromotionInProgress is a reason which
	// indicates that a promotion is in progress.
	DataPlaneConditionReasonRolloutPromotionInProgress consts.ConditionReason = "PromotionInProgress"

	// DataPlaneConditionReasonRolloutPromotionFailed is a reason which indicates
	// a DataPlane has failed to promote. This may be caused for example by
	// a failure in updating a live Service.
	DataPlaneConditionReasonRolloutPromotionFailed consts.ConditionReason = "PromotionFailed"

	// DataPlaneConditionReasonRolloutPromotionDone is a reason which indicates that a promotion is done.
	DataPlaneConditionReasonRolloutPromotionDone consts.ConditionReason = "PromotionDone"
)

const (
	// DataPlaneConditionMessageRolledOutRolloutInitialized contains the message
	// that is set for the RolledOut Condition when Reason is Progressing
	// and the DataPlane has initiated a rollout.
	DataPlaneConditionMessageRolledOutRolloutInitialized = "Rollout initialized"

	// DataPlaneConditionMessageRolledOutPreviewDeploymentNotYetReady contains the message
	// that is set for the RolledOut Condition when Reason is Progressing
	// and the operator is waiting for preview Deployment to be ready.
	DataPlaneConditionMessageRolledOutPreviewDeploymentNotYetReady = "Preview Deployment not yet ready"
)
