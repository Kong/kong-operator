package konnect

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

// ReferencedControlPlaneDoesNotExistError is an error type that is returned when
// a Konnect entity references a KonnectGatewayControlPlane that does not exist.
type ReferencedControlPlaneDoesNotExistError struct {
	Reference commonv1alpha1.ControlPlaneRef
	Err       error
}

// Error implements the error interface.
func (e ReferencedControlPlaneDoesNotExistError) Error() string {
	return fmt.Sprintf("referenced Control Plane %q does not exist: %v",
		e.Reference.String(), e.Err,
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

// ReferencedKongGatewayControlPlaneIsUnsupported is an error type that is returned when a given CP reference type is not
// supported.
type ReferencedKongGatewayControlPlaneIsUnsupported struct {
	Reference commonv1alpha1.ControlPlaneRef
}

func (e ReferencedKongGatewayControlPlaneIsUnsupported) Error() string {
	return fmt.Sprintf("referenced ControlPlaneRef %s is unsupported", e.Reference.String())
}
