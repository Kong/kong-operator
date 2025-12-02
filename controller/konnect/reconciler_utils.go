package konnect

import (
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// handleUpdateError handles common transient errors after Update operations.
// Returns (result, error, shouldReturn) where shouldReturn indicates if the caller
// should immediately return with the provided result and error.
//   - For conflict errors: returns (ctrl.Result{Requeue: true}, nil, true)
//   - For not found errors: returns (ctrl.Result{}, nil, true)
//   - For other errors: returns (ctrl.Result{}, err, true)
//   - For nil error: returns (ctrl.Result{}, nil, false)
func handleUpdateError(err error) (ctrl.Result, error, bool) {
	if err == nil {
		return ctrl.Result{}, nil, false
	}
	switch {
	case k8serrors.IsConflict(err):
		return ctrl.Result{Requeue: true}, nil, true
	case k8serrors.IsNotFound(err):
		return ctrl.Result{}, nil, true
	default:
		return ctrl.Result{}, err, true
	}
}
