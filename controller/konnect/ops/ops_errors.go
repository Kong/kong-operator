package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
)

// ErrNilResponse is an error indicating that a Konnect operation returned an empty response.
// This can sometimes happen regardless of the err being nil.
var ErrNilResponse = errors.New("nil response received")

type entity interface {
	client.Object
	GetTypeName() string
}

// EntityWithMatchingUIDNotFoundError is an error indicating that an entity with a matching UID was not found on Konnect.
type EntityWithMatchingUIDNotFoundError struct {
	Entity entity
}

// Error implements the error interface.
func (e EntityWithMatchingUIDNotFoundError) Error() string {
	return fmt.Sprintf(
		"%s %s (matching UID %s) not found on Konnect",
		e.Entity.GetTypeName(), e.Entity.GetName(), e.Entity.GetUID(),
	)
}

// CantPerformOperationWithoutControlPlaneIDError is an error indicating that an
// operation cannot be performed without a ControlPlane ID.
type CantPerformOperationWithoutControlPlaneIDError struct {
	Entity entity
	Op     Op
}

// Error implements the error interface.
func (e CantPerformOperationWithoutControlPlaneIDError) Error() string {
	return fmt.Sprintf(
		"can't %s %s %s without a Konnect ControlPlane ID",
		e.Op, e.Entity.GetTypeName(), client.ObjectKeyFromObject(e.Entity),
	)
}

type sdkErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details []struct {
		TypeAt   string   `json:"@type"`
		Type     string   `json:"type"`
		Field    string   `json:"field"`
		Messages []string `json:"messages"`
	} `json:"details"`
}

// ParseSDKErrorBody parses the body of an SDK error response.
// Exemplary body:
//
//	{
//		"code": 3,
//		"message": "data constraint error",
//		"details": [
//			{
//				"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
//				"type": "ERROR_TYPE_REFERENCE",
//				"field": "name",
//				"messages": [
//					"name (type: unique) constraint failed"
//				]
//			}
//		]
//	}
func ParseSDKErrorBody(body string) (sdkErrorBody, error) {
	var sdkErr sdkErrorBody
	if err := json.Unmarshal([]byte(body), &sdkErr); err != nil {
		return sdkErr, err
	}

	return sdkErr, nil
}

// ErrorIsCreateConflict returns true if the provided error is a Konnect conflict error.
//
// NOTE: Konnect APIs specific for Konnect only APIs like Gateway Control Planes
// return 409 conflict for already existing entities and return ConflictError.
// APIs that are shared with Kong Admin API return 400 for conflicts and return SDKError.
func ErrorIsCreateConflict(err error) bool {
	var (
		errConflict *sdkkonnecterrs.ConflictError
		sdkError    *sdkkonnecterrs.SDKError
	)
	if errors.As(err, &errConflict) {
		return true
	}
	if errors.As(err, &sdkError) {
		return SDKErrorIsConflict(sdkError)
	}
	return false
}

// SDKErrorIsConflict returns true if the provided SDKError indicates a conflict.
func SDKErrorIsConflict(sdkError *sdkkonnecterrs.SDKError) bool {
	sdkErrorBody, err := ParseSDKErrorBody(sdkError.Body)
	if err != nil {
		return false
	}

	const (
		dataConstraintMesasge = "data constraint error"
	)

	if sdkErrorBody.Message != dataConstraintMesasge {
		return false
	}

	switch sdkErrorBody.Code {
	case 3, 6:
		return true
	}
	return false
}

// handleDeleteError handles errors that occur during a delete operation.
// It logs a message and returns nil if the entity was not found in Konnect (when
// the delete operation is skipped).
func handleDeleteError[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, err error, ent TEnt) error {
	logDeleteSkipped := func() {
		ctrllog.FromContext(ctx).
			Info("entity not found in Konnect, skipping delete",
				"op", DeleteOp,
				"type", ent.GetTypeName(),
				"id", ent.GetKonnectStatus().GetKonnectID(),
			)
	}

	var sdkNotFoundError *sdkkonnecterrs.NotFoundError
	if errors.As(err, &sdkNotFoundError) {
		logDeleteSkipped()
		return nil
	}

	var sdkError *sdkkonnecterrs.SDKError
	if errors.As(err, &sdkError) {
		if sdkError.StatusCode == 404 {
			logDeleteSkipped()
			return nil
		}
		return FailedKonnectOpError[T]{
			Op:  DeleteOp,
			Err: sdkError,
		}
	}
	return FailedKonnectOpError[T]{
		Op:  DeleteOp,
		Err: err,
	}
}
