package konnect

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

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
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongUpstreamIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced Kong Upstream %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedKongUpstreamDoesNotExist is an error type that is returned when
// a Konnect entity references a Kong Upstream which does not exist.
type ReferencedKongUpstreamDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongUpstreamDoesNotExist) Error() string {
	return fmt.Sprintf("referenced Kong Upstream %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongCertificateIsBeingDeleted is an error type that is returned when
// a Konnect entity references a Kong Certificate which is being deleted.
type ReferencedKongCertificateIsBeingDeleted struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongCertificateIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced Kong Certificate %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedKongCertificateDoesNotExist is an error type that is returned when
// a Konnect entity references a Kong Certificate which does not exist.
type ReferencedKongCertificateDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongCertificateDoesNotExist) Error() string {
	return fmt.Sprintf("referenced Kong Certificate %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongKeySetDoesNotExist is an error type that is returned when
// a Konnect entity references a KongKeySet which does not exist.
type ReferencedKongKeySetDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedKongKeySetDoesNotExist) Error() string {
	return fmt.Sprintf("referenced KongKeySet %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedKongKeySetIsBeingDeleted is an error type that is returned when
// a Konnect entity references a KongKeySet which is being deleted.
type ReferencedKongKeySetIsBeingDeleted struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedKongKeySetIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced KongKeySet %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedObjectDoesNotExist is an error type that is returned when
// a Konnect entity references a non existing object.
type ReferencedObjectDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedObjectDoesNotExist) Error() string {
	return fmt.Sprintf("referenced object %s does not exist: %v", e.Reference, e.Err)
}

// ReferencedObjectIsBeingDeleted is an error type that is returned when
// a Konnect entity references an object which is being deleted.
type ReferencedObjectIsBeingDeleted struct {
	Reference         types.NamespacedName
	DeletionTimestamp time.Time
}

// Error implements the error interface.
func (e ReferencedObjectIsBeingDeleted) Error() string {
	return fmt.Sprintf("referenced object %s is being deleted (deletion timestamp: %s)",
		e.Reference, e.DeletionTimestamp)
}

// ReferencedObjectIsInvalid is an error type that is returned when
// the referenced object is invalid.
type ReferencedObjectIsInvalid struct {
	Reference string
	Msg       string
}

// Error implements the error interface.
func (e ReferencedObjectIsInvalid) Error() string {
	return fmt.Sprintf("referenced object %s is invalid: %v", e.Reference, e.Msg)
}

// ReferencedSecretDoesNotExist is an error type that is returned when
// a Konnect entity references a Secret which does not exist.
type ReferencedSecretDoesNotExist struct {
	Reference types.NamespacedName
	Err       error
}

// Error implements the error interface.
func (e ReferencedSecretDoesNotExist) Error() string {
	return fmt.Sprintf("referenced Secret %s does not exist: %v", e.Reference, e.Err)
}
