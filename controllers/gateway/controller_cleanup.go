package gateway

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controllers/pkg/log"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
)

// ----------------------------------------------------------------------------
// GatewayReconciler - Cleanup
// ----------------------------------------------------------------------------

func (r *Reconciler) cleanup(
	ctx context.Context,
	logger logr.Logger,
	gateway *gatewayv1.Gateway,
) (
	bool, // whether or not cleanup is being performed
	ctrl.Result,
	error,
) {
	if !gateway.DeletionTimestamp.IsZero() {
		if gateway.DeletionTimestamp.After(time.Now()) {
			log.Debug(logger, "gateway deletion still under grace period", gateway)
			return true, ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Until(gateway.DeletionTimestamp.Time),
			}, nil
		}
		log.Trace(logger, "gateway is marked delete, waiting for owned resources deleted", gateway)

		// delete owned dataplanes.
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, r.Client, gateway)
		if err != nil {
			return true, ctrl.Result{}, err
		}

		if len(dataplanes) > 0 {
			deletions, err := r.ensureOwnedDataPlanesDeleted(ctx, gateway)
			if err != nil {
				return true, ctrl.Result{}, err
			}
			if deletions {
				log.Debug(logger, "deleted owned dataplanes", gateway)
				return true, ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupDataPlanes)) {
				err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return true, ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up dataplanes removed", gateway)
				return true, ctrl.Result{}, nil
			}
		}

		// delete owned controlplanes.
		// Because controlplanes have finalizers, so we only remove the finalizer
		// for cleaning up owned controlplanes when they disappeared.
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
		if err != nil {
			return true, ctrl.Result{}, err
		}
		if len(controlplanes) > 0 {
			deletions, err := r.ensureOwnedControlPlanesDeleted(ctx, gateway)
			if err != nil {
				return true, ctrl.Result{}, err
			}
			if deletions {
				log.Debug(logger, "deleted owned controlplanes", gateway)
				return true, ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupControlPlanes)) {
				err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return true, ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up controlplanes removed", gateway)
				return true, ctrl.Result{}, nil
			}
		}

		// delete owned network policies
		networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway)
		if err != nil {
			return true, ctrl.Result{}, err
		}
		if len(networkPolicies) > 0 {
			deletions, err := r.ensureOwnedNetworkPoliciesDeleted(ctx, gateway)
			if err != nil {
				return true, ctrl.Result{}, err
			}
			if deletions {
				log.Debug(logger, "deleted owned network policies", gateway)
				return true, ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if controllerutil.RemoveFinalizer(gateway, string(GatewayFinalizerCleanupNetworkpolicies)) {
				err := r.Client.Patch(ctx, gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return true, ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up network policies removed", gateway)
				return true, ctrl.Result{}, nil
			}
		}

		log.Debug(logger, "owned resources cleanup completed", gateway)
		return true, ctrl.Result{}, nil
	}

	log.Debug(logger, "no cleanup required for Gateway", gateway)
	return false, ctrl.Result{}, nil
}
