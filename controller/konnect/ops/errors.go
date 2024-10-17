package ops

import (
	"fmt"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/pkg/consts"
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
	Reason    consts.ConditionReason
	Err       error
}

// Error implements the error interface.
func (e KonnectEntityCreatedButRelationsFailedError) Error() string {
	return fmt.Sprintf("Konnect entity (ID: %s) created but relations failed: %s: %v", e.KonnectID, e.Reason, e.Err)
}
