package controlplane

import (
	"fmt"

	commonv1alpha1 "github.com/kong/kong-operator/apis/common/v1alpha1"
)

// ReferencedControlPlaneDoesNotExistError is an error type that is returned when
// a Konnect entity references a KonnectGatewayControlPlane that does not exist.
type ReferencedControlPlaneDoesNotExistError struct {
	Reference commonv1alpha1.ControlPlaneRef
	Err       error
}

// Error implements the error interface.
func (e ReferencedControlPlaneDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Control Plane %q does not exist: %v",
		e.Reference.String(), e.Err,
	)
}

// Unwrap returns the underlying error.
func (e ReferencedControlPlaneDoesNotExistError) Unwrap() error {
	return e.Err
}

// ReferencedKongGatewayControlPlaneIsUnsupported is an error type that is returned when a given CP reference type is not
// supported.
type ReferencedKongGatewayControlPlaneIsUnsupported struct {
	Reference commonv1alpha1.ControlPlaneRef
}

func (e ReferencedKongGatewayControlPlaneIsUnsupported) Error() string {
	return fmt.Sprintf("referenced ControlPlaneRef %s is unsupported", e.Reference.String())
}
