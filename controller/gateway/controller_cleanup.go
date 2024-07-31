package gateway

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller/pkg/log"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
)

// ----------------------------------------------------------------------------
// Reconciler - Cleanup
// ----------------------------------------------------------------------------

const requeueWithoutBackoff = 200 * time.Millisecond

// cleanup determines whether cleanup is needed/underway for a Gateway and
// performs all necessary cleanup steps. Namely, it cleans up resources
// managed on behalf of the Gateway and removes the finalizers once all
// cleanup is finished so that the garbage collector can remove the resource.
func (r *Reconciler) cleanup(
	ctx context.Context,
	logger logr.Logger,
	gateway *gatewayv1.Gateway,
) (
	bool, // Whether the caller should return early from the reconciliation loop.
	ctrl.Result,
	error,
) {
	if gateway.DeletionTimestamp.IsZero() {
		log.Trace(logger, "no cleanup required for Gateway", gateway)
		return false, ctrl.Result{}, nil
	}

	if gateway.DeletionTimestamp.After(time.Now()) {
		log.Debug(logger, "gateway deletion still under grace period", gateway)
		return false, ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Until(gateway.DeletionTimestamp.Time),
		}, nil
	}
	log.Trace(logger, "gateway is marked delete, waiting for owned resources deleted", gateway)

	// Delete owned controlplanes.
	// Because controlplanes have finalizers, so we only remove the finalizer
	// for cleaning up owned controlplanes when they disappeared.
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if len(controlplanes) > 0 {
		deletions, err := r.ensureOwnedControlPlanesDeleted(ctx, gateway)
		if err != nil {
			return false, ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "deleted owned controlplanes", gateway)
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, err
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupControlPlanes)) {
			if err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := handleGatewayFinalizerPatchOrUpdateError(err, gateway, logger)
				return false, res, err
			}
			log.Debug(logger, "finalizer for cleaning up controlplanes removed", gateway)
			// Requeue to ensure that we continue reconciliation in case the patch
			// was empty and didn't trigger a new reconcile.
			return false, ctrl.Result{Requeue: true}, nil
		}
	}

	// Delete owned dataplanes.
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, ctrl.Result{}, err
	}

	if len(dataplanes) > 0 {
		deletions, err := r.ensureOwnedDataPlanesDeleted(ctx, gateway)
		if err != nil {
			return false, ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "deleted owned dataplanes", gateway)
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, err
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupDataPlanes)) {
			if err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := handleGatewayFinalizerPatchOrUpdateError(err, gateway, logger)
				return false, res, err
			}
			log.Debug(logger, "finalizer for cleaning up dataplanes removed", gateway)
			// Requeue to ensure that we continue reconciliation in case the patch
			// was empty and didn't trigger a new reconcile.
			return false, ctrl.Result{Requeue: true}, nil
		}
	}

	// Delete owned network policies
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if len(networkPolicies) > 0 {
		deletions, err := r.ensureOwnedNetworkPoliciesDeleted(ctx, gateway)
		if err != nil {
			return false, ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "deleted owned network policies", gateway)
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, err
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupNetworkpolicies)) {
			if err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := handleGatewayFinalizerPatchOrUpdateError(err, gateway, logger)
				return true, res, err
			}
			log.Debug(logger, "finalizer for cleaning up network policies removed", gateway)
			// Requeue to ensure that we continue reconciliation in case the patch
			// was empty and didn't trigger a new reconcile.
			return false, ctrl.Result{Requeue: true}, nil
		}
	}

	log.Debug(logger, "owned resources cleanup completed", gateway)
	return false, ctrl.Result{}, nil
}

func handleGatewayFinalizerPatchOrUpdateError(err error, gateway *gatewayv1.Gateway, logger logr.Logger) (ctrl.Result, error) {
	// Short cirtcuit.
	if err == nil {
		return ctrl.Result{}, nil
	}

	// If the Gateway is not found or there's a conflict patching, then requeue without an error.
	if k8serrors.IsNotFound(err) || k8serrors.IsConflict(err) {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: requeueWithoutBackoff,
		}, nil
	}

	// Since controllers use cached clients, it's possible that the Gateway is out of sync with what
	// is in the API server and this causes:
	// Forbidden: no new finalizers can be added if the object is being deleted, found new finalizers []string{...}
	// Code below handles that gracefully to not show users the errors that are not actionable.
	if cause, ok := k8serrors.StatusCause(err, metav1.CauseTypeForbidden); k8serrors.IsInvalid(err) && ok {
		log.Debug(logger, "failed to delete a finalizer on Gateway, requeueing request", gateway, "cause", cause)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: requeueWithoutBackoff,
		}, nil
	}

	// Return the error as is.
	return ctrl.Result{}, err
}
