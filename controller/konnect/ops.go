package konnect

import (
	"context"
	"fmt"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/log"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// Op is the Konnect operation type.
type Op string

const (
	// CreateOp is the Konnect create operation.
	CreateOp Op = "create"
	// UpdateOp is the Konnect update operation.
	UpdateOp Op = "update"
	// DeleteOp is the Konnect delete operation.
	DeleteOp Op = "delete"
)

// Create creates a Konnect entity.
func Create[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](ctx context.Context, sdk *sdkkonnectgo.SDK, logger logr.Logger, cl client.Client, e *T) (*T, error) {
	defer logOpComplete[T, TEnt](logger, time.Now(), CreateOp, e)

	switch ent := any(e).(type) { //nolint:gocritic
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

	switch ent := any(e).(type) { //nolint:gocritic
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

	switch ent := any(e).(type) { //nolint:gocritic
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
