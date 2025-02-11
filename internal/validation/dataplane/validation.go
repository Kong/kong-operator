package dataplane

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
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
	return nil
}
