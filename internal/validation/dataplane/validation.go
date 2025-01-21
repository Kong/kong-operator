package dataplane

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

// Validator validates DataPlane objects.
type Validator struct {
	c client.Client
}

// NewValidator creates a DataPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{c: c}
}

// Validate validates a DataPlane object and return the first validation error found.
func (v *Validator) Validate(dataplane *operatorv1beta1.DataPlane) error {
	if err := v.ValidateDataPlaneDeploymentRollout(dataplane.Spec.Deployment.Rollout); err != nil {
		return err
	}

	return nil
}

// ValidateDataPlaneDeploymentRollout validates the Rollout field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentRollout(rollout *operatorv1beta1.Rollout) error {
	if rollout != nil && rollout.Strategy.BlueGreen != nil &&
		rollout.Strategy.BlueGreen.Resources.Plan.Deployment == operatorv1beta1.RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout {
		// Can't use DeleteOnPromotionRecreateOnRollout just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1010.
		return errors.New("DataPlane Deployment resource plan DeleteOnPromotionRecreateOnRollout cannot be used yet")
	}

	return nil
}
