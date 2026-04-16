package konnect

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// handleEventGatewayRefResult handles the generic reconciler flow after
// handleEventGatewayRef resolved the Event Gateway reference.
func (r *KonnectEntityReconciler[T, TEnt]) handleEventGatewayRefResult(
	ctx context.Context,
	ent TEnt,
	res ctrl.Result,
	err error,
) (bool, ctrl.Result, error) {
	if err != nil {
		if errDel, ok := errors.AsType[ReferencedObjectIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return true, ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		if _, ok := errors.AsType[ReferencedObjectDoesNotExistError](err); ok {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return true, ctrl.Result{Requeue: true}, nil
					}
					if apierrors.IsNotFound(err) {
						return true, ctrl.Result{}, nil
					}
					return true, ctrl.Result{}, fmt.Errorf(
						"failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err,
					)
				}
			}
			return true, ctrl.Result{}, nil
		}

		res, err = patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
		return true, res, err
	}

	if res.IsZero() {
		return false, ctrl.Result{}, nil
	}

	if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
		return true, ctrl.Result{}, errStatus
	}

	return true, res, nil
}
