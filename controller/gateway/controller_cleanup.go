package gateway

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/controller/pkg/finalizer"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/patch"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
)

// ----------------------------------------------------------------------------
// Reconciler - Cleanup
// ----------------------------------------------------------------------------

// cleanup determines whether cleanup is needed/underway for a Gateway and
// performs all necessary cleanup steps.
// Namely, it cleans up resources managed on behalf of the Gateway and removes
// the finalizers one by one, after each cleanup step is finished.
// If the Gateway is marked for deletion, it will wait for all owned resources
// to be deleted before removing the finalizers.
//
// It returns a boolean indicating whether the caller should return early
// from the reconciliation loop, a ctrl.Result to requeue the request, and an error.
// The caller should return early if
//   - the requeue is set explicitly, then the ctrl.Result should be returned as is.
//   - the error is not nil, then the error should be returned as is.
//   - the boolean is true, then the reconciliation loop should return early without
//     requeuing the request.
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
		log.Trace(logger, "no cleanup required for Gateway")
		return false, ctrl.Result{}, nil
	}

	if gateway.DeletionTimestamp.After(time.Now()) {
		log.Debug(logger, "gateway deletion still under grace period")
		return false, ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Until(gateway.DeletionTimestamp.Time),
		}, nil
	}
	log.Trace(logger, "gateway is marked for deletion for owned resources to be deleted")

	if err := r.cleanupKonnectAPIAuthReferenceGrants(ctx, gateway); err != nil {
		return false, ctrl.Result{}, err
	}

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
			log.Debug(logger, "deleted owned controlplanes")
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, nil
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupControlPlanes)) {
			if err := r.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := finalizer.HandlePatchOrUpdateError(err, logger)
				return false, res, err
			}
			log.Debug(logger, "finalizer for cleaning up controlplanes removed")
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
			log.Debug(logger, "deleted owned dataplanes")
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, err
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupDataPlanes)) {
			if err := r.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := finalizer.HandlePatchOrUpdateError(err, logger)
				return false, res, err
			}
			log.Debug(logger, "finalizer for cleaning up dataplanes removed")
			// Requeue to ensure that we continue reconciliation in case the patch
			// was empty and didn't trigger a new reconcile.
			return false, ctrl.Result{Requeue: true}, nil
		}
	}

	konnectGatewayControlPlanes, err := gatewayutils.ListKonnectGatewayControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	for _, kgcp := range konnectGatewayControlPlanes {
		patched, res, err := patch.WithoutFinalizer(ctx, r.Client, &kgcp, KonnectGatewayControlPlaneFinalizer)
		if patched || err != nil || !res.IsZero() {
			return false, res, err
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
			log.Debug(logger, "deleted owned network policies")
			// Return early from reconciliation, deletion will trigger a new reconcile.
			return true, ctrl.Result{}, err
		}
	} else {
		oldGateway := gateway.DeepCopy()
		if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupNetworkPolicies)) {
			if err := r.Patch(ctx, gateway, client.MergeFrom(oldGateway)); err != nil {
				res, err := finalizer.HandlePatchOrUpdateError(err, logger)
				return true, res, err
			}
			log.Debug(logger, "finalizer for cleaning up network policies removed")
			// Requeue to ensure that we continue reconciliation in case the patch
			// was empty and didn't trigger a new reconcile.
			return false, ctrl.Result{Requeue: true}, nil
		}
	}

	log.Debug(logger, "owned resources cleanup completed")
	return true, ctrl.Result{}, nil
}
