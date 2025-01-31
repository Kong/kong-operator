package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/log"
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

type sdkErrorDetails struct {
	TypeAt   string   `json:"@type"`
	Type     string   `json:"type"`
	Field    string   `json:"field"`
	Messages []string `json:"messages"`
}

type sdkErrorBody struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Details []sdkErrorDetails `json:"details"`
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

const (
	dataConstraintMesasge   = "data constraint error"
	validationErrorMessage  = "validation error"
	apiErrorOccurredMessage = "API error occurred"
)

// ErrorIsSDKErrorTypeField returns true if the provided error is a type field error.
// These types of errors are unrecoverable and should be covered by CEL validation
// rules on CRDs but just in case some of those are left unhandled we can handle
// them by reacting to SDKErrors of type ERROR_TYPE_FIELD.
//
// Exemplary body:
//
//	{
//		"code": 3,
//		"message": "validation error",
//		"details": [
//			{
//				"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
//				"type": "ERROR_TYPE_FIELD",
//				"field": "tags[0]",
//				"messages": [
//					"length must be <= 128, but got 138"
//				]
//			}
//		]
//	}
func ErrorIsSDKErrorTypeField(err error) bool {
	var errSDK *sdkkonnecterrs.SDKError
	if !errors.As(err, &errSDK) {
		return false
	}

	errSDKBody, err := ParseSDKErrorBody(errSDK.Body)
	if err != nil {
		return false
	}

	switch errSDKBody.Message {
	case validationErrorMessage:
		if !slices.ContainsFunc(errSDKBody.Details, func(d sdkErrorDetails) bool {
			return d.Type == "ERROR_TYPE_FIELD"
		}) {
			return false
		}

		return true
	default:
		return false
	}
}

// ErrorIsSDKError403 returns true if the provided error is a 403 Forbidden error.
// This can happen when the requested operation is not permitted.
// Example SDKError body (SDKError message is a separate field from body message):
//
//	{
//		"code": 7,
//		"message": "usage constraint error",
//		"details": [
//			{
//				"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
//				"messages": [
//					"operation not permitted on KIC cluster"
//				]
//			}
//		]
//	}
func ErrorIsSDKError403(err error) bool {
	var errSDK *sdkkonnecterrs.SDKError
	if !errors.As(err, &errSDK) {
		return false
	}

	return errSDK.StatusCode == 403
}

// ErrorIsSDKBadRequestError returns true if the provided error is a BadRequestError.
func ErrorIsSDKBadRequestError(err error) bool {
	var errSDK *sdkkonnecterrs.BadRequestError
	return errors.As(err, &errSDK)
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

	if sdkErrorBody.Message != dataConstraintMesasge {
		return false
	}

	switch sdkErrorBody.Code {
	case 3, 6:
		return true
	}
	return false
}

func errIsNotFound(err error) bool {
	var (
		notFoundError *sdkkonnecterrs.NotFoundError
		sdkError      *sdkkonnecterrs.SDKError
	)
	return errors.As(err, &notFoundError) ||
		errors.As(err, &sdkError) && sdkError.StatusCode == http.StatusNotFound
}

// handleUpdateError handles errors that occur during an update operation.
// If the entity is not found, then it uses the provided create function to
// recreate the it.
func handleUpdateError[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	err error,
	ent TEnt,
	createFunc func(ctx context.Context) error,
) error {
	if errIsNotFound(err) {
		id := ent.GetKonnectStatus().GetKonnectID()
		logEntityNotFoundRecreating(ctx, ent, id)
		if createErr := createFunc(ctx); createErr != nil {
			return FailedKonnectOpError[T]{
				Op:  CreateOp,
				Err: fmt.Errorf("failed to create %s %s: %w", ent.GetTypeName(), id, createErr),
			}
		}
		return nil
	}
	return FailedKonnectOpError[T]{
		Op:  UpdateOp,
		Err: err,
	}
}

// handleDeleteError handles errors that occur during a delete operation.
// It logs a message and returns nil if the entity was not found in Konnect (when
// the delete operation is skipped).
func handleDeleteError[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ctx context.Context, err error, ent TEnt) error {
	if errIsNotFound(err) {
		ctrllog.FromContext(ctx).
			Info("entity not found in Konnect, skipping delete",
				"op", DeleteOp,
				"type", ent.GetTypeName(),
				"id", ent.GetKonnectStatus().GetKonnectID(),
			)
		return nil
	}

	return FailedKonnectOpError[T]{
		Op:  DeleteOp,
		Err: err,
	}
}

// IgnoreUnrecoverableAPIErr ignores unrecoverable errors that would cause the
// reconciler to endlessly requeue.
func IgnoreUnrecoverableAPIErr(err error, logger logr.Logger) error {
	// If the error is a type field error or bad request error, then don't propagate
	// it to the caller.
	// We cannot recover from this error as this requires user to change object's
	// manifest. The entity's status is already updated with the error.
	if ErrorIsSDKErrorTypeField(err) ||
		ErrorIsSDKBadRequestError(err) ||
		ErrorIsSDKError403(err) {
		log.Debug(logger, "ignoring unrecoverable API error, consult object's status for details", "err", err)
		return nil
	}

	return err
}
