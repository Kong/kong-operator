package konnect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/log"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// Response is the interface for the response from the Konnect API.
type Response interface {
	GetContentType() string
	GetStatusCode() int
	GetRawResponse() *http.Response
}

// Op is the type for the operation type of a Konnect entity.
type Op string

const (
	// CreateOp is the operation type for creating a Konnect entity.
	CreateOp Op = "create"
	// UpdateOp is the operation type for updating a Konnect entity.
	UpdateOp Op = "update"
	// DeleteOp is the operation type for deleting a Konnect entity.
	DeleteOp Op = "delete"
)

// Create creates a Konnect entity.
func Create[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](ctx context.Context, sdk *sdkkonnectgo.SDK, logger logr.Logger, cl client.Client, e *T) (*T, error) {
	defer logOpComplete[T, TEnt](logger, time.Now(), CreateOp, e)

	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectControlPlane:
		return e, createControlPlane(ctx, sdk, logger, ent)

		// ---------------------------------------------------------------------
		// TODO: add other Konnect types

	default:
		return nil, fmt.Errorf("unsupported entity type %T", ent)
	}
}

// Delete deletes a Konnect entity.
func Delete[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](ctx context.Context, sdk *sdkkonnectgo.SDK, logger logr.Logger, cl client.Client, e *T) error {
	defer logOpComplete[T, TEnt](logger, time.Now(), DeleteOp, e)

	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectControlPlane:
		return deleteControlPlane(ctx, sdk, logger, ent)

		// ---------------------------------------------------------------------
		// TODO: add other Konnect types

	default:
		return fmt.Errorf("unsupported entity type %T", ent)
	}
}

// Update updates a Konnect entity.
func Update[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](ctx context.Context, sdk *sdkkonnectgo.SDK, logger logr.Logger, cl client.Client, e *T) (ctrl.Result, error) {
	var (
		ent                = TEnt(e)
		condProgrammed, ok = k8sutils.GetCondition(KonnectEntityProgrammedConditionType, ent)
		now                = time.Now()
		timeFromLastUpdate = time.Since(condProgrammed.LastTransitionTime.Time)
	)
	// If the entity is already programmed and the last update was less than
	// the configured sync period, requeue after the remaining time.
	if ok &&
		condProgrammed.Status == metav1.ConditionTrue &&
		condProgrammed.Reason == KonnectEntityProgrammedReason &&
		condProgrammed.ObservedGeneration == ent.GetObjectMeta().GetGeneration() &&
		timeFromLastUpdate <= configurableSyncPeriod {
		requeueAfter := configurableSyncPeriod - timeFromLastUpdate
		log.Debug(logger, "no need for update, requeueing after configured sync period", e,
			"last_update", condProgrammed.LastTransitionTime.Time,
			"time_from_last_update", timeFromLastUpdate,
			"requeue_after", requeueAfter,
			"requeue_at", now.Add(requeueAfter),
		)
		return ctrl.Result{
			RequeueAfter: requeueAfter,
		}, nil
	}

	defer logOpComplete[T, TEnt](logger, now, UpdateOp, e)

	switch ent := any(e).(type) {
	case *konnectv1alpha1.KonnectControlPlane:
		return ctrl.Result{}, updateControlPlane(ctx, sdk, logger, ent)

		// ---------------------------------------------------------------------
		// TODO: add other Konnect types

	default:
		return ctrl.Result{}, fmt.Errorf("unsupported entity type %T", ent)
	}
}

func logOpComplete[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](logger logr.Logger, start time.Time, op Op, e TEnt) {
	logger.Info("operation in Konnect API complete",
		"op", op,
		"duration", time.Since(start),
		"type", entityTypeName[T](),
		"konnect_id", e.GetKonnectStatus().GetKonnectID(),
	)
}

// handleResp checks the response from the Konnect API and returns an error if
// the response is not successful.
// It closes the response body.
func handleResp[T SupportedKonnectEntityType](err error, resp Response, op Op) error {
	if err != nil {
		return err
	}
	body := resp.GetRawResponse().Body
	defer body.Close()
	if resp.GetStatusCode() < 200 || resp.GetStatusCode() >= 400 {
		b, err := io.ReadAll(body)
		if err != nil {
			var e T
			return fmt.Errorf(
				"failed to %s %T and failed to read response body: %w",
				op, e, err,
			)
		}
		var e T
		return fmt.Errorf("failed to %s %T: %s", op, e, string(b))
	}
	return nil
}
