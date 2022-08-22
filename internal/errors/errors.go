package errors

import (
	"errors"
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

// ErrUnsupportedGateway is an error which indicates that a provided Gateway
// is not supported because it's GatewayClass was not associated with this
// controller.
var ErrUnsupportedGateway = errors.New("gateway not supported")

// ErrTooManyDataPlaneNetworkPolicies is an error which indicates that a DataPlane
// has too many NetworkPolicies configured.
var ErrTooManyDataPlaneNetworkPolicies = errors.New("too many data plane network policies")

// -----------------------------------------------------------------------------
// GatewayClass - Errors
// -----------------------------------------------------------------------------

// ErrObjectMissingParametersRef is a custom error that must be used when the
// .spec.ParametersRef field of the given object is nil
var ErrObjectMissingParametersRef = errors.New("no reference to related objects")

// -----------------------------------------------------------------------------
// Controlplane - Errors
// -----------------------------------------------------------------------------

// ErrDataPlaneNotSet is a custom error that must be used when a specific OwnerReference
// is expected to be on an object, but it is not found.
var ErrDataPlaneNotSet = errors.New("no dataplane name set")
