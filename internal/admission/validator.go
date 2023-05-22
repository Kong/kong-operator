package admission

import (
	"context"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	controlplanevalidation "github.com/kong/gateway-operator/internal/validation/controlplane"
	dataplanevalidation "github.com/kong/gateway-operator/internal/validation/dataplane"
)

type validator struct {
	dataplaneValidator    *dataplanevalidation.Validator
	controlplaneValidator *controlplanevalidation.Validator
}

func (v *validator) ValidateControlPlane(ctx context.Context, controlPlane operatorv1alpha1.ControlPlane) error {
	return v.controlplaneValidator.Validate(&controlPlane)
}

func (v *validator) ValidateDataPlane(ctx context.Context, dataPlane operatorv1alpha1.DataPlane) error {
	return v.dataplaneValidator.Validate(&dataPlane)
}
