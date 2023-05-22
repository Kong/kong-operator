package dataplane

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

// Validator validates ControlPlane objects.
type Validator struct{}

// NewValidator creates a ControlPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{}
}

// Validate validates a ControlPlane object and return the first validation error found.
func (v *Validator) Validate(controlplane *operatorv1alpha1.ControlPlane) error {
	if err := v.ValidateDeploymentOptions(&controlplane.Spec.Deployment); err != nil {
		return err
	}

	// prepared for more validations
	return nil
}

// ValidateDeploymentOptions validates the DeploymentOptions field of ControlPlane object.
func (v *Validator) ValidateDeploymentOptions(opts *operatorv1alpha1.DeploymentOptions) error {
	// Ref: https://github.com/Kong/gateway-operator/issues/736
	if opts.Replicas != nil && *opts.Replicas != 1 {
		return errors.New("ControlPlanes only support replicas of 1")
	}
	// Ref: https://github.com/Kong/gateway-operator/issues/740
	if len(opts.Pods.Volumes) > 0 || len(opts.Pods.VolumeMounts) > 0 {
		return errors.New("ControlPlanes does not support custom volumes and volume mounts")
	}

	// Ref: https://github.com/Kong/gateway-operator/issues/754.
	if opts.Pods.ContainerImage == nil || len(*opts.Pods.ContainerImage) == 0 {
		return errors.New("ControlPlanes requires a containerImage")
	}
	if opts.Pods.Version == nil || len(*opts.Pods.Version) == 0 {
		return errors.New("ControlPlanes requires a version")
	}

	return nil
}
