package errors

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// Objects conversion - Errors
// -----------------------------------------------------------------------------

// ErrUnexpectedObject is a custom error that must be used when the cast of a object to an expected
// type fails.
var ErrUnexpectedObject = errors.New("unexpected object type provided")

// -----------------------------------------------------------------------------
// Gateway - Errors
// -----------------------------------------------------------------------------

// UnsupportedGatewayClassError is an error which indicates that a provided GatewayClass
// is not supported.
type UnsupportedGatewayClassError struct {
	reason string
}

// NewErrUnsupportedGateway creates a new ErrUnsupportedGatewayClass error.
func NewErrUnsupportedGateway(reason string) UnsupportedGatewayClassError {
	return UnsupportedGatewayClassError{reason: reason}
}

// Error returns the error message for the ErrUnsupportedGatewayClass error.
func (e UnsupportedGatewayClassError) Error() string {
	return fmt.Sprintf("unsupported gateway class: %s", e.reason)
}

// NotAcceptedGatewayClassError is an error which indicates that a provided GatewayClass
// is not accepted.
type NotAcceptedGatewayClassError struct {
	gatewayClass string
	condition    metav1.Condition
}

// NewErrNotAcceptedGatewayClass creates a new ErrNotAcceptedGatewayClass error.
func NewErrNotAcceptedGatewayClass(gatewayClass string, condition metav1.Condition) NotAcceptedGatewayClassError {
	return NotAcceptedGatewayClassError{gatewayClass: gatewayClass, condition: condition}
}

// Error returns the error message for the ErrNotAcceptedGatewayClass error.
func (e NotAcceptedGatewayClassError) Error() string {
	return fmt.Sprintf("gateway class %s not accepted; reason: %s, message: %s", e.gatewayClass, e.condition.Reason, e.condition.Message)
}

// -----------------------------------------------------------------------------
// ControlPlane - Errors
// -----------------------------------------------------------------------------

// ErrDataPlaneNotSet is a custom error that must be used when a specific OwnerReference
// is expected to be on an object, but it is not found.
var ErrDataPlaneNotSet = errors.New("no dataplane name set")

// ErrNoDataPlanePods is a custom error that must be used when the DataPlane Deployment
// referenced by the ControlPlane has no pods ready yet.
var ErrNoDataPlanePods = errors.New("no dataplane pods existing yet")

// -----------------------------------------------------------------------------
// Version Strings - Errors
// -----------------------------------------------------------------------------

// ErrInvalidSemverVersion is a custom error that indicates a provided
// version string (which we were expecting to be in the format of
// <Major>.<Minor>.<Patch>) was invalid, and not in the expected format.
var ErrInvalidSemverVersion = errors.New("not a valid semver version")
