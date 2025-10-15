package finalizer

import (
	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	ctrlconsts "github.com/kong/kong-operator/controller/consts"
	"github.com/kong/kong-operator/controller/pkg/log"
)

// HandlePatchOrUpdateError handles errors returned from patch or update operations
// on Kubernetes resources when changing finalizers.
func HandlePatchOrUpdateError(err error, logger logr.Logger) (ctrl.Result, error) {
	// Short circuit.
	if err == nil {
		return ctrl.Result{}, nil
	}

	if k8serrors.IsNotFound(err) {
		log.Debug(logger, "object not found when updating/patching")
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// When there's a conflict when updating/patching, requeue without an error.
	if k8serrors.IsConflict(err) {
		log.Debug(logger, "conflict found when updating/patching, retrying")
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// Since controllers use cached clients, it's possible that the object is out of sync with what
	// is in the API server and this causes:
	// Forbidden: no new finalizers can be added if the object is being deleted, found new finalizers []string{...}
	// Code below handles that gracefully to not show users the errors that are not actionable.
	if cause, ok := k8serrors.StatusCause(err, metav1.CauseTypeForbidden); k8serrors.IsInvalid(err) && ok {
		log.Debug(logger, "failed to delete a finalizer, requeueing request", "cause", cause)
		return ctrl.Result{
			RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
		}, nil
	}

	// Return the error as is.
	return ctrl.Result{}, err
}
