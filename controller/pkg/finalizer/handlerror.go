package finalizer

import (
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

const deletingObjectFinalizerMutationMessage = "no new finalizers can be added if the object is being deleted"

// HandlePatchOrUpdateError handles errors returned from patch or update operations
// on Kubernetes resources when changing finalizers.
func HandlePatchOrUpdateError(err error, logger logr.Logger) (ctrl.Result, error) {
	// Short circuit.
	if err == nil {
		return ctrl.Result{}, nil
	}

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "object not found when updating/patching")
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// When there's a conflict when updating/patching, requeue without an error.
	if apierrors.IsConflict(err) {
		log.Debug(logger, "conflict found when updating/patching, retrying")
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// Since controllers use cached clients, it is possible to race with object deletion
	// and attempt to write a stale finalizer set that would add finalizers back.
	// Requeue only for this specific case so the next reconcile refreshes object state.
	if cause, ok := apierrors.StatusCause(err, metav1.CauseTypeForbidden); isDeletingObjectFinalizerMutationError(err) && ok {
		log.Debug(logger, "stale finalizer mutation rejected for deleting object, requeueing with refreshed state", "cause", cause)
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// Return the error as is.
	return ctrl.Result{}, err
}

func isDeletingObjectFinalizerMutationError(err error) bool {
	if !apierrors.IsInvalid(err) {
		return false
	}

	cause, ok := apierrors.StatusCause(err, metav1.CauseTypeForbidden)
	if !ok {
		return false
	}

	return strings.Contains(cause.Message, deletingObjectFinalizerMutationMessage)
}
