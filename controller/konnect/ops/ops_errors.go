package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/log"
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

// EntityWithMatchingIDNotFoundError is an error indicating that an entity with the matching ID was not found on Konnect.
type EntityWithMatchingIDNotFoundError struct {
	ID string
}

// Error implements the error interface.
func (e EntityWithMatchingIDNotFoundError) Error() string {
	return fmt.Sprintf("entity with ID %s not found on Konnect", e.ID)
}

// MultipleEntitiesWithMatchingIDFoundError is an error indicating that multiple entities with the same ID were found on Konnect.
type MultipleEntitiesWithMatchingIDFoundError struct {
	ID string
}

// Error implements the error interface.
func (e MultipleEntitiesWithMatchingIDFoundError) Error() string {
	return fmt.Sprintf("multiple entities with ID %s found on Konnect", e.ID)
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

// CantPerformOperationWithoutNetworkIDError is an error indicating that an
// operation cannot be performed without a Konnect network ID.
type CantPerformOperationWithoutNetworkIDError struct {
	Entity entity
	Op     Op
}

func (e CantPerformOperationWithoutNetworkIDError) Error() string {
	return fmt.Sprintf(
		"can't %s %s %s without a Konnect Cloud Gateway Network ID",
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

// ErrorIsSDKError400 returns true if the provided error is a 400 BadRequestError.
// This can happen when the requested entity fails the validation.
func ErrorIsSDKError400(err error) bool {
	var errSDK *sdkkonnecterrs.SDKError
	if !errors.As(err, &errSDK) {
		return false
	}

	return errSDK.StatusCode == 400
}

// ErrorIsConflictError returns true if the provided error is a 409 ConflictError.
// This can happen when the entity already exists.
// Example error body:
//
//	{
//		"status": 409,
//		"title": "Conflict",
//		"instance": "kong:trace:14893476519012495000",
//		"detail": "Key (org_id, name)=(8a6e97b1-1111-1111-1111-111111111111, test1) already exists."
//	}
func ErrorIsConflictError(err error) bool {
	var errConflict *sdkkonnecterrs.ConflictError
	if !errors.As(err, &errConflict) {
		return false
	}

	if !errConflictHasStatusCode(errConflict, 409) {
		return false
	}

	return true
}

func errConflictHasStatusCode(err *sdkkonnecterrs.ConflictError, n int) bool {
	if err == nil {
		return false
	}
	// NOTE: Status contains a float64 value, so we need to cast it to int to deterministically compare.
	floatStatus, okStatus := (err.Status).(float64)
	return okStatus && int(floatStatus) == n
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
// We currently handle two codes (as mapped in
// https://grpc.io/docs/guides/status-codes/#the-full-list-of-status-codes)
// from SDK error:
// - 3: INVALID_ARGUMENT
// - 6: ALREADY_EXISTS
//
// Example error body:
//
//	{
//	"code": 3,
//	"message": "name (type: unique) constraint failed",
//	"details": [
//	  {
//	  "@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
//	  "type": "ERROR_TYPE_REFERENCE",
//	  "field": "name",
//	  "messages": [
//	    "name (type: unique) constraint failed"
//	  ]
//	  }
//	]
//	}
func SDKErrorIsConflict(sdkError *sdkkonnecterrs.SDKError) bool {
	sdkErrorBody, err := ParseSDKErrorBody(sdkError.Body)
	if err != nil {
		return false
	}

	switch sdkErrorBody.Code {
	case 3: // INVALID_ARGUMENT
		const (
			dataConstraintMesasge      = "data constraint error"
			typeUniqueConstraintFailed = "(type: unique) constraint failed"
		)

		if sdkErrorBody.Message == dataConstraintMesasge ||
			strings.Contains(sdkErrorBody.Message, typeUniqueConstraintFailed) {
			return true
		}

	case 6: // ALREADY_EXISTS
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
	if ErrorIsSDKBadRequestError(err) ||
		ErrorIsSDKError400(err) ||
		ErrorIsSDKError403(err) ||
		ErrorIsConflictError(err) {
		log.Debug(logger, "ignoring unrecoverable API error, consult object's status for details", "err", err)
		return nil
	}

	return err
}

func errorIsDataPlaneGroupConflictProposedConfigIsTheSame(err error) bool {
	var errConflict *sdkkonnecterrs.ConflictError
	if !errors.As(err, &errConflict) {
		return false
	}

	if !errConflictHasStatusCode(errConflict, 409) {
		return false
	}

	strDetail, okDetail := errConflict.Detail.(string)
	if !okDetail ||
		!strings.Contains(
			strDetail, "Proposed configuration and current configuration are identical",
		) {
		return false
	}

	return true
}

func errorIsDataPlaneGroupBadRequestPreviousConfigNotFinishedProvisioning(err error) bool {
	var errBadRequest *sdkkonnecterrs.BadRequestError
	if !errors.As(err, &errBadRequest) {
		return false
	}

	const (
		errMsgConfigSameAsPrevious = "Data-plane groups in the previous configuration have not finished provisioning"
		errInvalidParameterField   = "dataplane_groups"
	)

	return lo.ContainsBy(
		errBadRequest.InvalidParameters,
		func(p sdkkonnectcomp.InvalidParameters) bool {
			return p.Type == sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard &&
				p.InvalidParameterStandard != nil &&
				p.InvalidParameterStandard.Field == errInvalidParameterField &&
				strings.Contains(p.InvalidParameterStandard.Reason, errMsgConfigSameAsPrevious)
		})
}
