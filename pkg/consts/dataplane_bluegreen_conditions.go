package consts

import kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"

const (
	// DataPlaneConditionTypeRolledOut is a condition type indicating whether or
	// not, DataPlane's rollout has been successful or not.
	DataPlaneConditionTypeRolledOut kcfgconsts.ConditionType = "RolledOut"
)

const (
	// DataPlaneConditionReasonRolloutAwaitingPromotion is a reason which indicates a DataPlane
	// preview has been deployed successfully and is awaiting promotion.
	// If this Reason is present and no automated rollout is disabled, user can
	// use the preview services and deployment to inspect the state of those
	// make a judgement call if the promotion should happen.
	DataPlaneConditionReasonRolloutAwaitingPromotion kcfgconsts.ConditionReason = "AwaitingPromotion"

	// DataPlaneConditionReasonRolloutFailed is a reason which indicates a DataPlane
	// has failed to roll out. This may be caused for example by a Deployment or
	// a Service failing to get created during a rollout.
	DataPlaneConditionReasonRolloutFailed kcfgconsts.ConditionReason = "Failed"

	// DataPlaneConditionReasonRolloutProgressing is a reason which indicates a DataPlane's
	// new version is being rolled out.
	DataPlaneConditionReasonRolloutProgressing kcfgconsts.ConditionReason = "Progressing"

	// DataPlaneConditionReasonRolloutWaitingForChange is a reason which indicates a DataPlane
	// is waiting for a change to trigger new version to be made available before promotion.
	DataPlaneConditionReasonRolloutWaitingForChange kcfgconsts.ConditionReason = "WaitingForChange"

	// DataPlaneConditionReasonRolloutPromotionInProgress is a reason which
	// indicates that a promotion is in progress.
	DataPlaneConditionReasonRolloutPromotionInProgress kcfgconsts.ConditionReason = "PromotionInProgress"

	// DataPlaneConditionReasonRolloutPromotionFailed is a reason which indicates
	// a DataPlane has failed to promote. This may be caused for example by
	// a failure in updating a live Service.
	DataPlaneConditionReasonRolloutPromotionFailed kcfgconsts.ConditionReason = "PromotionFailed"

	// DataPlaneConditionReasonRolloutPromotionDone is a reason which indicates that a promotion is done.
	DataPlaneConditionReasonRolloutPromotionDone kcfgconsts.ConditionReason = "PromotionDone"
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
