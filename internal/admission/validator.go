package admission

import (
	"context"
	"errors"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	dataplanevalidation "github.com/kong/gateway-operator/internal/validation/dataplane"
)

type validator struct {
	dataplaneValidator *dataplanevalidation.Validator
}

func (v *validator) ValidateControlPlane(ctx context.Context, controlPlane operatorv1alpha1.ControlPlane) error {
	// Ref: https://github.com/Kong/gateway-operator/issues/736
	if controlPlane.Spec.Deployment.Replicas != nil && *controlPlane.Spec.Deployment.Replicas != 1 {
		return errors.New("ControlPlanes only support replicas of 1")
	}
	// Ref: https://github.com/Kong/gateway-operator/issues/740
	if len(controlPlane.Spec.Deployment.Volumes) > 0 || len(controlPlane.Spec.Deployment.VolumeMounts) > 0 {
		return errors.New("ControlPlanes does not support custom volumes and volume mounts")
	}
	return nil
}

func (v *validator) ValidateDataPlane(ctx context.Context, dataPlane operatorv1alpha1.DataPlane) error {
	return v.dataplaneValidator.Validate(&dataPlane)
}
