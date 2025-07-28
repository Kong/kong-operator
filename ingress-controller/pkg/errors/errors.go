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
