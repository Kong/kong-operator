package errors

import (
	"fmt"

	k8stypes "k8s.io/apimachinery/pkg/types"
)

// NoAvailableEndpointsError is returned when there are no endpoints available for a service.
type NoAvailableEndpointsError struct {
	serviceNN k8stypes.NamespacedName
}

// NewNoAvailableEndpointsError creates a new NoAvailableEndpointsError.
func NewNoAvailableEndpointsError(serviceNN k8stypes.NamespacedName) NoAvailableEndpointsError {
	return NoAvailableEndpointsError{serviceNN: serviceNN}
}

// Error implements the error interface for NoAvailableEndpointsError.
func (e NoAvailableEndpointsError) Error() string {
	return fmt.Sprintf("no endpoints for service: %q", e.serviceNN)
}

// KongClientNotReadyError is returned when the Kong client is not ready to be used yet.
// This can happen if the Kong Admin API is not reachable, or if it's reachable but `GET /status` does not return 200.
type KongClientNotReadyError struct {
	Err error
}

// Error implements the error interface for KongClientNotReadyError.
func (e KongClientNotReadyError) Error() string {
	return fmt.Sprintf("client not ready: %s", e.Err)
}

// Unwrap allows access to the underlying error wrapped by KongClientNotReadyError.
func (e KongClientNotReadyError) Unwrap() error {
	return e.Err
}

// Is reports any error in err's tree matches target.
func (e KongClientNotReadyError) Is(target error) bool {
	_, ok := target.(KongClientNotReadyError)
	return ok
}
