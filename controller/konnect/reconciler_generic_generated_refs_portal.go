package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
)

func (r *KonnectEntityReconciler[T, TEnt]) handleGeneratedPortalRef(
	ctx context.Context,
	ent TEnt,
) (bool, ctrl.Result, error) {
	res, err := handlePortalRef(ctx, r.Client, ent)
	return r.handlePortalRefResult(ctx, ent, res, err)
}

// handlePortalRefResult handles the generic reconciler flow after
// handlePortalRef resolved the Portal reference.
func (r *KonnectEntityReconciler[T, TEnt]) handlePortalRefResult(
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
						return true, ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
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
