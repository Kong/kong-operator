package ops

import (
	"fmt"
	"time"

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
