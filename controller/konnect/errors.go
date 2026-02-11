package konnect

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// ReferencedKongServiceIsBeingDeletedError is an error type that is returned when
// a Konnect entity references a Kong Service which is being deleted.
type ReferencedKongServiceIsBeingDeletedError struct {
	Reference types.NamespacedName
}

// Error implements the error interface.
func (e ReferencedKongServiceIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced Kong Service %s is being deleted", e.Reference)
}

// ReferencedKongConsumerIsBeingDeletedError is an error type that is returned when
// a Konnect entity references a Kong Consumer which is being deleted.
type ReferencedKongConsumerIsBeingDeletedError struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongConsumerIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced Kong Consumer %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp,
	)
}

// ReferencedKongConsumerDoesNotExistError is an error type that is returned when the referenced KongConsumer does not exist.
type ReferencedKongConsumerDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongConsumerDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Kong Consumer %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongUpstreamIsBeingDeletedError is an error type that is returned when
// a Konnect entity references a Kong Upstream which is being deleted.
type ReferencedKongUpstreamIsBeingDeletedError struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongUpstreamIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced Kong Upstream %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedKongUpstreamDoesNotExistError is an error type that is returned when
// a Konnect entity references a Kong Upstream which does not exist.
type ReferencedKongUpstreamDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongUpstreamDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Kong Upstream %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongCertificateIsBeingDeletedError is an error type that is returned when
// a Konnect entity references a Kong Certificate which is being deleted.
type ReferencedKongCertificateIsBeingDeletedError struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongCertificateIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced Kong Certificate %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedKongCertificateDoesNotExistError is an error type that is returned when
// a Konnect entity references a Kong Certificate which does not exist.
type ReferencedKongCertificateDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongCertificateDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Kong Certificate %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongKeySetDoesNotExistError is an error type that is returned when
// a Konnect entity references a KongKeySet which does not exist.
type ReferencedKongKeySetDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongKeySetDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced KongKeySet %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongKeySetIsBeingDeletedError is an error type that is returned when
// a Konnect entity references a KongKeySet which is being deleted.
type ReferencedKongKeySetIsBeingDeletedError struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongKeySetIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced KongKeySet %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedObjectDoesNotExistError is an error type that is returned when
// a Konnect entity references a non existing object.
type ReferencedObjectDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedObjectDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced object %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedObjectIsBeingDeletedError is an error type that is returned when
// a Konnect entity references an object which is being deleted.
type ReferencedObjectIsBeingDeletedError struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedObjectIsBeingDeletedError) Error() string {
	return fmt.Sprintf("referenced object %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedObjectIsInvalidError is an error type that is returned when
// the referenced object is invalid.
type ReferencedObjectIsInvalidError struct {
	Reference string
	Msg       string
}

// Error implements the error interface.
func (e ReferencedObjectIsInvalidError) Error() string {
	return fmt.Sprintf("referenced object %s is invalid: %v", e.Reference, e.Msg)
}

// ReferencedSecretDoesNotExistError is an error type that is returned when
// a Konnect entity references a Secret which does not exist.
type ReferencedSecretDoesNotExistError struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedSecretDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Secret %s does not exist: %v", e.Reference, e.Err)
}
