package konnect

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// ReferencedControlPlaneDoesNotExistError is an error type that is returned when
// a Konnect entity references a KonnectGatewayControlPlane that does not exist.
type ReferencedControlPlaneDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedControlPlaneDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Control Plane %s does not exist: %v",
		e.Reference, e.Err,
	)
}

// Unwrap returns the underlying error.
func (e ReferencedControlPlaneDoesNotExistError) Unwrap() error {
	return e.Err
}

// ReferencedKongServiceIsBeingDeleted is an error type that is returned when
// a Konnect entity references a Kong Service which is being deleted.
type ReferencedKongServiceIsBeingDeleted struct {
	Reference types.NamespacedName
}

// Error implements the error interface.
func (e ReferencedKongServiceIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced Kong Service %s is being deleted", e.Reference)
}

// ReferencedKongConsumerIsBeingDeleted is an error type that is returned when
// a Konnect entity references a Kong Consumer which is being deleted.
type ReferencedKongConsumerIsBeingDeleted struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongConsumerIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced Kong Consumer %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp,
	)
}

// ReferencedKongConsumerDoesNotExist is an error type that is returned when the referenced KongConsumer does not exist.
type ReferencedKongConsumerDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongConsumerDoesNotExist) Error() string {
	return fmt.Sprintf("referenced Kong Consumer %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongUpstreamIsBeingDeleted is an error type that is returned when
// a Konnect entity references a Kong Upstream which is being deleted.
type ReferencedKongUpstreamIsBeingDeleted struct {
	Reference types.NamespacedName
}

// Error implements the error interface.
func (e ReferencedKongUpstreamIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced Kong Upstream %s is being deleted", e.Reference)
}
