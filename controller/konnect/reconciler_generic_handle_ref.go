package konnect

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// handleRefResult handles the generic reconciler flow after handle<TYPE>Ref
// resolved the <TYPE> reference.
func handleRefResult(
	ctx context.Context,
	cl client.Client,
	ent k8sutils.ConditionsAwareObject,
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
				if err := cl.Update(ctx, ent); err != nil {
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

		// The referenced parent is not Konnect-managed, so the entity is not either:
		// stop without error and without touching Konnect. The cleanup finalizer is
		// removed defensively: a stale finalizer would block the entity's deletion.
		// An entity that previously reconciled under a Konnect parent (objects that
		// predate the AIGatewayRef kind field) leaves its Konnect entity orphaned.
		if _, ok := errors.AsType[ReferencedObjectNotKonnectManagedError](err); ok {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := cl.Update(ctx, ent); err != nil {
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

		res, err = patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, cl, ent)
		return true, res, err
	}

	if res.IsZero() {
		return false, ctrl.Result{}, nil
	}

	if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, cl, ent); errStatus != nil {
		return true, ctrl.Result{}, errStatus
	}

	return true, res, nil
}
