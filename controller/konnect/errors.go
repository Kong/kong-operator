package konnect

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// FailedKonnectOpError is an error type that is returned when an operation against
// Konnect API fails.
type FailedKonnectOpError[T SupportedKonnectEntityType] struct {
	Op  Op
	Err error
}

// Error implements the error interface.
func (e FailedKonnectOpError[T]) Error() string {
	return fmt.Sprintf("failed to %s %s on Konnect: %v",
		e.Op, entityTypeName[T](), e.Err,
	)
}

// Unwrap returns the underlying error.
func (e FailedKonnectOpError[T]) Unwrap() error {
	return e.Err
}

// ReferencedControlPlaneDoesNotExistError is an error type that is returned when
// a Konnect entity references a KonnectControlPlane that does not exist.
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
