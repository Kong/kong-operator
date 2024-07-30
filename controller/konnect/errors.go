package konnect

import "fmt"

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
