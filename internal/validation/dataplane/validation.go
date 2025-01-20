package dataplane

import (
	"errors"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// Validator validates DataPlane objects.
type Validator struct {
	c client.Client
}

// NewValidator creates a DataPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{c: c}
}

// ValidateUpdate validates a DataPlane object change upon an update event.
func (v *Validator) ValidateUpdate(dataplane, oldDataPlane *operatorv1beta1.DataPlane) error {
	return v.ValidateIfRolloutInProgress(dataplane, oldDataPlane)
}

// Validate validates a DataPlane object and return the first validation error found.
func (v *Validator) Validate(dataplane *operatorv1beta1.DataPlane) error {
	err := v.ValidateDataPlaneDeploymentOptions(dataplane.Namespace, &dataplane.Spec.Deployment.DeploymentOptions)
	if err != nil {
		return err
	}

	if err := v.ValidateDataPlaneDeploymentRollout(dataplane.Spec.Deployment.Rollout); err != nil {
		return err
	}

	if dataplane.Spec.Network.Services != nil && dataplane.Spec.Network.Services.Ingress != nil &&
		dataplane.Spec.Deployment.PodTemplateSpec != nil {
		proxyContainer := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		if err := v.ValidateDataPlaneIngressServiceOptions(dataplane.Namespace, dataplane.Spec.Network.Services.Ingress, proxyContainer); err != nil {
			return err
		}
	}

	return nil
}

// ValidateDataPlaneDeploymentRollout validates the Rollout field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentRollout(rollout *operatorv1beta1.Rollout) error {
	if rollout != nil && rollout.Strategy.BlueGreen != nil && rollout.Strategy.BlueGreen.Promotion.Strategy == operatorv1beta1.AutomaticPromotion {
		// Can't use AutomaticPromotion just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1006.
		return errors.New("DataPlane AutomaticPromotion cannot be used yet")
	}

	if rollout != nil && rollout.Strategy.BlueGreen != nil &&
		rollout.Strategy.BlueGreen.Resources.Plan.Deployment == operatorv1beta1.RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout {
		// Can't use DeleteOnPromotionRecreateOnRollout just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1010.
		return errors.New("DataPlane Deployment resource plan DeleteOnPromotionRecreateOnRollout cannot be used yet")
	}

	return nil
}

func (v *Validator) ValidateIfRolloutInProgress(dataplane, oldDataPlane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.RolloutStatus == nil {
		return nil
	}

	// If no rollout condition exists, the rollout is not started yet
	c, exists := k8sutils.GetCondition(consts.DataPlaneConditionTypeRolledOut, dataplane.Status.RolloutStatus)
	if !exists {
		return nil
	}

	// If the promotion is in progress and the spec is changed in the update
	// then reject the change.
	if c.Reason == string(consts.DataPlaneConditionReasonRolloutPromotionInProgress) &&
		!cmp.Equal(dataplane.Spec, oldDataPlane.Spec) {
		return ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion
	}

	return nil
}

// ValidateDataPlaneDeploymentOptions validates the DeploymentOptions field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentOptions(namespace string, opts *operatorv1beta1.DeploymentOptions) error {
	return nil
}

// ValidateDataPlaneIngressServiceOptions validates spec.serviceOptions of given DataPlane.
func (v *Validator) ValidateDataPlaneIngressServiceOptions(
	namespace string, opts *operatorv1beta1.DataPlaneServiceOptions, proxyContainer *corev1.Container,
) error {
	return nil
}
