package controlplane

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

// Validator validates ControlPlane objects.
type Validator struct{}

// NewValidator creates a ControlPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{}
}

// Validate validates a ControlPlane object and return the first validation error found.
func (v *Validator) Validate(controlplane *operatorv1beta1.ControlPlane) error {
	if err := v.ValidateDeploymentOptions(&controlplane.Spec.Deployment); err != nil {
		return err
	}

	// prepared for more validations
	return nil
}

// ValidateDeploymentOptions validates the DeploymentOptions field of ControlPlane object.
func (v *Validator) ValidateDeploymentOptions(opts *operatorv1beta1.ControlPlaneDeploymentOptions) error {
	return nil
}
