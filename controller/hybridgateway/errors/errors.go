package errors

import (
	"errors"
	"fmt"
)

var (
	// ErrNoGatewayFound is returned when a referenced Gateway does not exist in the cluster.
	ErrNoGatewayFound = fmt.Errorf("no supported gateway found")

	// ErrNoGatewayClassFound is returned when a GatewayClass referenced by a Gateway does not exist in the cluster.
	ErrNoGatewayClassFound = fmt.Errorf("no gatewayClass found for gateway")

	// ErrNoGatewayController is returned when a GatewayClass exists but is not managed by this controller.
	ErrNoGatewayController = fmt.Errorf("gatewayClass is not managed by this controller")

	// ErrUnsupportedKind is returned when a ParentRef references an unsupported resource kind.
	ErrUnsupportedKind = fmt.Errorf("unsupported ParentRef kind, only Gateway is supported")

	// ErrUnsupportedGroup is returned when a ParentRef references an unsupported API group.
	ErrUnsupportedGroup = fmt.Errorf("unsupported ParentRef group, only gateway.networking.k8s.io is supported")

	// ErrKonnectExtensionCrossNamespaceReference is returned when a KonnectExtension references a ControlPlane in a different namespace.
	ErrKonnectExtensionCrossNamespaceReference = fmt.Errorf("cross-namespace references between KonnectExtension and ControlPlane are not supported")

	// ErrGatewayNotReferencingControlPlane is returned when a Gateway does not reference a ControlPlane.
	ErrGatewayNotReferencingControlPlane = fmt.Errorf("gateway does not reference a ControlPlane")

	// ErrMalformedAnnotation is returned when a konghq.com/* annotation value
	// fails to parse (invalid bool, invalid integer, negative integer, etc.).
	ErrMalformedAnnotation = fmt.Errorf("malformed annotation")
)

// ClientCertificateRefError denotes invalid desired client authentication, not an API failure.
// Translation may omit the affected rule and remove its old bindings, but must never
// replace it with a KongService without the requested client certificate.
type ClientCertificateRefError struct {
	Reason  string
	Message string
}

func (e *ClientCertificateRefError) Error() string {
	return e.Message
}

// OnlyClientCertificateRefErrors distinguishes invalid bindings from transient failures,
// including errors wrapped by the converters' aggregated translation errors.
func OnlyClientCertificateRefErrors(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		errs := e.Unwrap()
		if len(errs) == 0 {
			return false
		}
		for _, child := range errs {
			if !OnlyClientCertificateRefErrors(child) {
				return false
			}
		}
		return true
	}
	_, ok := errors.AsType[*ClientCertificateRefError](err)
	return ok
}
