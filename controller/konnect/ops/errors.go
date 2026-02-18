package ops

import (
	"fmt"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
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
