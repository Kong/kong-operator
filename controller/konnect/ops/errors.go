package ops

import (
	"fmt"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	"github.com/kong/kong-operator/controller/konnect/constraints"
)

// FailedKonnectOpError is an error type that is returned when an operation against
// Konnect API fails.
type FailedKonnectOpError[T constraints.SupportedKonnectEntityType] struct {
	Op  Op
	Err error
}

// Error implements the error interface.
func (e FailedKonnectOpError[T]) Error() string {
	return fmt.Sprintf("failed to %s %s on Konnect: %v",
		e.Op, constraints.EntityTypeName[T](), e.Err,
	)
}

// Unwrap returns the underlying error.
func (e FailedKonnectOpError[T]) Unwrap() error {
	return e.Err
}

// KonnectEntityCreatedButRelationsFailedError is an error type that is returned when
// an entity is created successfully in Konnect (an ID is assigned) but subsequent operations
// to create relations to the entity fail.
type KonnectEntityCreatedButRelationsFailedError struct {
	KonnectID string
	Reason    kcfgconsts.ConditionReason
	Err       error
}

// Error implements the error interface.
func (e KonnectEntityCreatedButRelationsFailedError) Error() string {
	return fmt.Sprintf("Konnect entity (ID: %s) created but relations failed: %s: %v", e.KonnectID, e.Reason, e.Err)
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityCreatedButRelationsFailedError) Is(target error) bool {
	if t, ok := target.(KonnectEntityCreatedButRelationsFailedError); ok {
		return e.KonnectID == t.KonnectID && e.Reason == t.Reason
	}
	return false
}

// GetControlPlaneGroupMemberFailedError is an error type returned when
// failed to get member of control plane group.
type GetControlPlaneGroupMemberFailedError struct {
	MemberName string
	Err        error
}

// Error implements the error interface.
func (e GetControlPlaneGroupMemberFailedError) Error() string {
	return fmt.Sprintf("failed to get control plane group member %s: %v", e.MemberName, e.Err.Error())
}

// Unwrap returns the underlying error.
func (e GetControlPlaneGroupMemberFailedError) Unwrap() error {
	return e.Err
}

// ControlPlaneGroupMemberNoKonnectIDError is an error type returned when
// member of control plane group does not have a Konnect ID.
type ControlPlaneGroupMemberNoKonnectIDError struct {
	GroupName  string
	MemberName string
}

// Error implements the error interface.
func (e ControlPlaneGroupMemberNoKonnectIDError) Error() string {
	return fmt.Sprintf("control plane group %s member %s has no Konnect ID", e.GroupName, e.MemberName)
}

// KonnectEntityAdoptionFetchError is an error type returned when failed to fetch the entity
// on trying to adopt an existing entity in Konnect.
type KonnectEntityAdoptionFetchError struct {
	KonnectID string
	Err       error
}

// Error implements the error interface.
func (e KonnectEntityAdoptionFetchError) Error() string {
	return fmt.Sprintf("failed to fetch Konnect entity (ID: %s) for adoption: %v", e.KonnectID, e.Err)
}

// Unwrap returns the underlying error.
func (e KonnectEntityAdoptionFetchError) Unwrap() error {
	return e.Err
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityAdoptionFetchError) Is(target error) bool {
	if t, ok := target.(KonnectEntityAdoptionFetchError); ok {
		return e.KonnectID == t.KonnectID
	}
	return false
}

// KonnectEntityAdoptionReferenceServiceIDMismatchError is an error type returned when
// adopting an existing entity but the reference service ID does not match.
type KonnectEntityAdoptionReferenceServiceIDMismatchError struct{}

// Error implements the error interface.
func (e KonnectEntityAdoptionReferenceServiceIDMismatchError) Error() string {
	return "failed to adopt: reference service ID does not match"
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityAdoptionReferenceServiceIDMismatchError) Is(target error) bool {
	_, ok := target.(KonnectEntityAdoptionReferenceServiceIDMismatchError)
	return ok
}

// KonnectEntityAdoptionRouteTypeNotSupportedError is an error type returned when
// adopting an existing entity but the route type is not supported.
type KonnectEntityAdoptionRouteTypeNotSupportedError struct {
	RouteType sdkkonnectcomp.RouteType
}

// Error implements the error interface.
func (e KonnectEntityAdoptionRouteTypeNotSupportedError) Error() string {
	return fmt.Sprintf("failed to adopt: route type %q not supported", e.RouteType)
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityAdoptionRouteTypeNotSupportedError) Is(target error) bool {
	if t, ok := target.(KonnectEntityAdoptionRouteTypeNotSupportedError); ok {
		return e.RouteType == t.RouteType
	}
	return false
}

// KonnectEntityAdoptionUIDTagConflictError is an error type returned in adopting an existing entity
// when the entity has a tag to note that the entity is managed by another object with a different UID.
type KonnectEntityAdoptionUIDTagConflictError struct {
	KonnectID    string
	ActualUIDTag string
}

// Error implements the error interface.
func (e KonnectEntityAdoptionUIDTagConflictError) Error() string {
	return fmt.Sprintf("Konnect entity (ID: %s) is managed by another k8s object with UID %s", e.KonnectID, e.ActualUIDTag)
}

// KonnectEntityAdoptionNotMatchError is an error type returned in adopting an existing entity
// in "match" mode but the configuration of the existing entity does not match the spec of the object.
type KonnectEntityAdoptionNotMatchError struct {
	KonnectID string
}

// Error implements the error interface.
func (e KonnectEntityAdoptionNotMatchError) Error() string {
	return fmt.Sprintf("Konnect entity (ID: %s) does not match the spec of the object when adopting in match mode", e.KonnectID)
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityAdoptionNotMatchError) Is(target error) bool {
	if t, ok := target.(KonnectEntityAdoptionNotMatchError); ok {
		return e.KonnectID == t.KonnectID
	}
	return false
}

// KonnectEntityAdoptionMissingControlPlaneIDError is an error type returned particular ControlPlane ID does not exist.
type KonnectEntityAdoptionMissingControlPlaneIDError struct{}

// Error implements the error interface.
func (e KonnectEntityAdoptionMissingControlPlaneIDError) Error() string {
	return "no Control Plane ID"
}

// Is reports any error in err's tree matches target.
func (e KonnectEntityAdoptionMissingControlPlaneIDError) Is(target error) bool {
	_, ok := target.(KonnectEntityAdoptionMissingControlPlaneIDError)
	return ok
}

// KonnectOperationFailedError is an error type returned when a Konnect API operation fails.
// It includes the operation type, entity type name, entity key, and underlying error.
type KonnectOperationFailedError struct {
	Op         Op
	EntityType string
	EntityKey  string
	Err        error
}

// Error implements the error interface.
func (e KonnectOperationFailedError) Error() string {
	return fmt.Sprintf("failed to %s %s %s: %v", e.Op, e.EntityType, e.EntityKey, e.Err)
}

func (e KonnectOperationFailedError) Unwrap() error {
	return e.Err
}

// Is reports any error in err's tree matches target.
func (e KonnectOperationFailedError) Is(target error) bool {
	if t, ok := target.(KonnectOperationFailedError); ok {
		return e.Op == t.Op &&
			e.EntityType == t.EntityType &&
			e.EntityKey == t.EntityKey &&
			(e.Err != nil && t.Err != nil &&
				e.Err.Error() == t.Err.Error() ||
				e.Err == nil && t.Err == nil)
	}
	return false
}

// RateLimitError is an error type returned when a Konnect API operation
// fails due to rate limiting (HTTP 429 Too Many Requests).
// It includes the retry-after duration to indicate when the operation can be retried.
type RateLimitError struct {
	Err        error
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e RateLimitError) Error() string {
	return fmt.Sprintf("rate limited by Konnect API, retry after %s: %v", e.RetryAfter, e.Err)
}

// Unwrap returns the underlying error.
func (e RateLimitError) Unwrap() error {
	return e.Err
}
