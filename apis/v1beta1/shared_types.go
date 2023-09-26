package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
)

// DeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the number of replicas
// or pod options like container image and resource requirements.
// version, as well as Env variable overrides.
type DeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This only affects the DataPlane deployments for now, for more details on
	// ControlPlane scaling please see https://github.com/Kong/gateway-operator/issues/736.
	//
	// +optional
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	// It's being applied on top of the generated Deployments using
	// [StrategicMergePatch](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch#StrategicMergePatch).
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// Rollout defines options for rollouts.
type Rollout struct {
	// Strategy contains the deployment strategy for rollout.
	Strategy RolloutStrategy `json:"strategy"`
}

// RolloutStrategy holds the rollout strategy options.
type RolloutStrategy struct {
	// BlueGreen holds the options specific for Blue Green Deployments.
	//
	// +optional
	BlueGreen *BlueGreenStrategy `json:"blueGreen,omitempty"`
}

// BlueGreenStrategy defines the Blue Green deployment strategy.
type BlueGreenStrategy struct {
	// Promotion defines how the operator handles promotion of resources.
	Promotion Promotion `json:"promotion"`

	// Resources controls what happens to operator managed resources during or
	// after a rollout.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={"plan":{"deployment":"ScaleDownOnPromotionScaleUpOnRollout"}}
	Resources RolloutResources `json:"resources,omitempty"`
}

// Promotion is a type that contains fields that define how the operator handles
// promotion of resources during a blue/green rollout.
type Promotion struct {
	// Strategy indicates how you want the operator to handle the promotion of
	// the preview (green) resources (Deployments and Services) after all workflows
	// and tests succeed, OR if you even want it to break before performing
	// the promotion to allow manual inspection.
	//
	// +kubebuilder:validation:Enum=AutomaticPromotion;BreakBeforePromotion
	// +kubebuilder:default=BreakBeforePromotion
	Strategy PromotionStrategy `json:"strategy"`
}

// PromotionStrategy is the type of promotion strategy consts.
//
// Allowed values:
//
//   - `BreakBeforePromotion` is a promotion strategy which will ensure all new
//     resources are ready and then break, to enable manual inspection.
//     The user must indicate manually when they want the promotion to continue.
//     That can be done by annotating the `DataPlane` object with
//     `"gateway-operator.konghq.com/promote-when-ready": "true"`.
type PromotionStrategy string

const (
	// AutomaticPromotion indicates that once all workflows and tests have completed successfully,
	// the new resources should be promoted and replace the previous resources.
	AutomaticPromotion PromotionStrategy = "AutomaticPromotion"

	// BreakBeforePromotion is the same as AutomaticPromotion but with an added breakpoint
	// to enable manual inspection.
	// The user must indicate manually when they want the promotion to continue.
	// That can be done by annotating the DataPlane object with
	// `"gateway-operator.konghq.com/promote-when-ready": "true"`.
	BreakBeforePromotion PromotionStrategy = "BreakBeforePromotion"
)

// RolloutResources is the type which contains the fields which control how the operator
// manages the resources it manages during or after the rollout concludes.
type RolloutResources struct {
	// Plan defines the resource plan for managing resources during and after a rollout.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={"deployment":"ScaleDownOnPromotionScaleUpOnRollout"}
	Plan RolloutResourcePlan `json:"plan,omitempty"`
}

// RolloutResourcePlan is a type that holds rollout resource plan related fields
// which control how the operator handles resources during and after a rollout.
type RolloutResourcePlan struct {
	// Deployment describes how the operator manages Deployments during and after a rollout.
	//
	// +kubebuilder:validation:Enum=ScaleDownOnPromotionScaleUpOnRollout;DeleteOnPromotionRecreateOnRollout
	// +kubebuilder:default=ScaleDownOnPromotionScaleUpOnRollout
	Deployment RolloutResourcePlanDeployment `json:"deployment,omitempty"`
}

// RolloutResourcePlanDeployment is the type that holds the resource plan for
// managing the Deployment objects during and after a rollout.
//
// Allowed values:
//
//   - `ScaleDownOnPromotionScaleUpOnRollout` is a rollout
//     resource plan for Deployment which makes the operator scale down
//     the Deployment to 0 when the rollout is not initiated by a spec change
//     and then to scale it up when the rollout is initiated (the owner resource
//     like a DataPlane is patched or updated).
type RolloutResourcePlanDeployment string

const (
	// RolloutResourcePlanDeploymentScaleDownOnPromotionScaleUpOnRollout is a rollout
	// resource plan for Deployment which makes the operator scale down
	// the Deployment to 0 when the rollout is not initiated by a spec change
	// and then to scale it up when the rollout is initiated (the owner resource
	// like a DataPlane is patched or updated).
	RolloutResourcePlanDeploymentScaleDownOnPromotionScaleUpOnRollout RolloutResourcePlanDeployment = "ScaleDownOnPromotionScaleUpOnRollout"
	// RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout which makes the operator delete the
	// Deployment the rollout is not initiated by a spec change and then to
	// re-create it when the rollout is initiated (the owner resource like
	// a DataPlane is patched or updated)
	RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout RolloutResourcePlanDeployment = "DeleteOnPromotionRecreateOnRollout"
)

// GatewayConfigurationTargetKind is an object kind that can be targeted for
// GatewayConfiguration attachment.
type GatewayConfigurationTargetKind string

const (
	// GatewayConfigurationTargetKindGateway is a target kind which indicates
	// that a Gateway resource is the target.
	GatewayConfigurationTargetKindGateway GatewayConfigurationTargetKind = "Gateway"

	// GatewayConfigurationTargetKindGatewayClass is a target kind which indicates
	// that a GatewayClass resource is the target.
	GatewayConfigurationTargetKindGatewayClass GatewayConfigurationTargetKind = "GatewayClass"
)

const (
	// DataPlanePromoteWhenReadyAnnotationKey is the annotation key which can be used
	// to annotate a DataPlane object to signal that the live resources should be
	// promoted and replace the preview resources. It is used in conjunction with
	// the BreakBeforePromotion promotion strategy.
	// It has to be set to `true` to take effect. Once the operator detects the annotation, it will proceed with the
	// promotion and remove the annotation.
	DataPlanePromoteWhenReadyAnnotationKey = "gateway-operator.konghq.com/promote-when-ready"

	// DataPlanePromoteWhenReadyAnnotationTrue is the annotation value that needs to be set to the DataPlane's
	// DataPlanePromoteWhenReadyAnnotationKey annotation to signal that the new resources should be promoted.
	DataPlanePromoteWhenReadyAnnotationTrue = "true"
)
