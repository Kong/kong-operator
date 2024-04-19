package admission

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	controlplanevalidation "github.com/kong/gateway-operator/internal/validation/controlplane"
	dataplanevalidation "github.com/kong/gateway-operator/internal/validation/dataplane"
)

type validator struct {
	dataplaneValidator    *dataplanevalidation.Validator
	controlplaneValidator *controlplanevalidation.Validator
}

// ValidateControlPlane validates the ControlPlane resource.
func (v *validator) ValidateControlPlane(ctx context.Context, controlPlane operatorv1beta1.ControlPlane) error {
	return v.controlplaneValidator.Validate(&controlPlane)
}

// ValidateDataPlane validates the DataPlane resource.
func (v *validator) ValidateDataPlane(ctx context.Context, dataPlane, old operatorv1beta1.DataPlane, operation admissionv1.Operation) error {
	switch operation {
	case admissionv1.Update, admissionv1.Create:
		if err := v.dataplaneValidator.Validate(&dataPlane); err != nil {
			return err
		}
		if operation == admissionv1.Update {
			if err := v.dataplaneValidator.ValidateUpdate(&dataPlane, &old); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}
