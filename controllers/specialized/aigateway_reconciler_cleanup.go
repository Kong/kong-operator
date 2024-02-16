package specialized

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers/pkg/log"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Cleanup
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) cleanup(
	ctx context.Context,
	logger logr.Logger,
	aigateway *v1alpha1.AIGateway,
) (
	bool, // whether or not cleanup is being performed or needs to be performed
	reconcile.Result,
	error,
) {
	if aigateway.DeletionTimestamp.IsZero() {
		log.Trace(logger, "no cleanup required for aigateway", aigateway)
		return false, ctrl.Result{}, nil
	}

	if aigateway.DeletionTimestamp.After(time.Now()) {
		log.Debug(logger, "aigateway deletion still under grace period", aigateway)
		return true, ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Until(aigateway.DeletionTimestamp.Time),
		}, nil
	}
	log.Trace(logger, "aigateway is marked for deletion, waiting for owned resources deleted", aigateway)

	// TODO: as we add new owned resources (e.g. KongPlugins, HTTPRoutes)
	// they will need to be added here.
	//
	// See: https://github.com/Kong/gateway-operator/issues/1429

	oldGateway := aigateway.DeepCopy()
	if controllerutil.RemoveFinalizer(aigateway, string(AIGatewayCleanupFinalizer)) {
		err := r.Client.Patch(ctx, aigateway, client.MergeFrom(oldGateway))
		if err != nil {
			return true, ctrl.Result{}, err
		}
		log.Debug(logger, "finalizer for cleaning up removed", aigateway)
		return true, ctrl.Result{}, nil
	}

	log.Debug(logger, "owned resources cleanup completed", aigateway)
	return true, ctrl.Result{}, nil
}
