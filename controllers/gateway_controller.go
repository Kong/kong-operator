package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler
// -----------------------------------------------------------------------------

// GatewayReconciler reconciles a Gateway object
type GatewayReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch Gateway objects, filtering out any Gateways which are not configured with
		// a supported GatewayClass controller name.
		For(&gwtypes.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayHasMatchingGatewayClass))).
		// watch for changes in dataplanes created by the gateway controller
		Owns(&operatorv1alpha1.DataPlane{}).
		// watch for changes in controlplanes created by the gateway controller
		Owns(&operatorv1alpha1.ControlPlane{}).
		// watch for changes in networkpolicies created by the gateway controller
		Owns(&networkingv1.NetworkPolicy{}).
		// watch for updates to GatewayConfigurations, if any configuration targets a
		// Gateway that is supported, enqueue that Gateway.
		Watches(
			&source.Kind{Type: &operatorv1alpha1.GatewayConfiguration{}},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayConfig),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayConfigurationMatchesController))).
		// watch for updates to GatewayClasses, if any GatewayClasses change, enqueue
		// reconciliation for all supported gateway objects which reference it.
		Watches(
			&source.Kind{Type: &gatewayv1beta1.GatewayClass{}},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayClass),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatchesController))).
		// watch for updates to Services which are owned by DataPlanes that
		// are owned by a Gateway.
		Watches(
			&source.Kind{Type: &corev1.Service{}},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForService)).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := getLogger(ctx, "gateway", r.DevelopmentMode)

	trace(log, "reconciling gateway resource", req)
	var gateway gwtypes.Gateway
	if err := r.Client.Get(ctx, req.NamespacedName, &gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !gateway.DeletionTimestamp.IsZero() {
		if gateway.DeletionTimestamp.After(time.Now()) {
			debug(log, "gateway deletion still under grace period", gateway)
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Until(gateway.DeletionTimestamp.Time),
			}, nil
		}
		trace(log, "gateway is marked delete, waiting for owned resources deleted", gateway)

		// delete owned dataplanes.
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, r.Client, &gateway)
		if err != nil {
			return ctrl.Result{}, err
		}

		if len(dataplanes) > 0 {
			deletions, err := r.ensureOwnedDataPlanesDeleted(ctx, &gateway)
			if err != nil {
				return ctrl.Result{}, err
			}
			if deletions {
				debug(log, "deleted owned dataplanes", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupDataPlanes)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				debug(log, "finalizer for cleaning up dataplanes removed", gateway)
				return ctrl.Result{}, nil
			}
		}

		// delete owned controlplanes.
		// Because controlplanes have finalizers, so we only remove the finalizer
		// for cleaning up owned controlplanes when they disappeared.
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, &gateway)
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(controlplanes) > 0 {
			deletions, err := r.ensureOwnedControlPlanesDeleted(ctx, &gateway)
			if err != nil {
				return ctrl.Result{}, err
			}
			if deletions {
				debug(log, "deleted owned controlplanes", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupControlPlanes)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				debug(log, "finalizer for cleaning up controlplanes removed", gateway)
				return ctrl.Result{}, nil
			}
		}

		// delete owned network policies
		networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, &gateway)
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(networkPolicies) > 0 {
			deletions, err := r.ensureOwnedNetworkPoliciesDeleted(ctx, &gateway)
			if err != nil {
				return ctrl.Result{}, err
			}
			if deletions {
				debug(log, "deleted owned network policies", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupNetworkpolicies)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				debug(log, "finalizer for cleaning up network policies removed", gateway)
				return ctrl.Result{}, nil
			}
		}

		// cleanup completed
		debug(log, "owned resource cleanup completed, gateway deleted", gateway)
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	finalizersChanged := k8sutils.EnsureFinalizersInMetadata(&gateway.ObjectMeta,
		string(GatewayFinalizerCleanupControlPlanes),
		string(GatewayFinalizerCleanupDataPlanes),
		string(GatewayFinalizerCleanupNetworkpolicies),
	)
	if finalizersChanged {
		trace(log, "update metadata of gateway to set finalizer", gateway)
		return ctrl.Result{}, r.Client.Update(ctx, &gateway)
	}

	trace(log, "checking gatewayclass", gateway)
	gwc, err := r.verifyGatewayClassSupport(ctx, &gateway)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrUnsupportedGateway) {
			debug(log, "resource not supported, ignoring", gateway, "ExpectedGatewayClass", vars.ControllerName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !gwc.isAccepted() {
		debug(log, "gatewayclass not accepted , ignoring", gateway)
		return ctrl.Result{}, nil
	}

	gwConditionAware := gatewayConditionsAware(&gateway)
	k8sutils.InitReady(gwConditionAware)

	trace(log, "resource is supported, ensuring that it gets marked as scheduled", gateway)
	if !k8sutils.IsValidCondition(GatewayScheduledType, gwConditionAware) {
		condition := k8sutils.NewCondition(
			k8sutils.ConditionType(gatewayv1beta1.GatewayConditionScheduled),
			metav1.ConditionTrue, k8sutils.ConditionReason(gatewayv1beta1.GatewayReasonScheduled),
			fmt.Sprintf("this gateway has been picked up by the %s and will be processed", vars.ControllerName),
		)
		k8sutils.SetCondition(condition, gwConditionAware)
	}

	trace(log, "determining configuration", gateway)
	gatewayConfig, err := r.getOrCreateGatewayConfiguration(ctx, gwc.GatewayClass)
	if err != nil {
		return ctrl.Result{}, err
	}

	oldGateway := gateway.DeepCopy()
	oldGwConditionsAware := gatewayConditionsAware(oldGateway)

	// Provision dataplane creates a dataplane and adds the DataPlaneReady=True
	// condition to the Gateway status if the dataplane is ready. If not ready
	// the status DataPlaneReady=False will be set instead.
	dataplane := r.provisionDataPlane(ctx, log, &gateway, gatewayConfig)

	// Set the DataPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no DataPlaneReady condition in the old gateway, or
	// * the new status is false and the previous status was true
	if !k8sutils.IsValidCondition(DataPlaneReadyType, gwConditionAware) {
		condition, found := k8sutils.GetCondition(DataPlaneReadyType, oldGwConditionsAware)
		if !found || condition.Status == metav1.ConditionTrue {
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil { // requeue will be triggered by the update of the dataplane status
				return ctrl.Result{}, err
			}
			debug(log, "dataplane not ready yet", gateway)
		}
		return ctrl.Result{}, nil
	}
	// if the dataplane wasnt't ready before this reconciliation loop and now is ready, log this event
	if !k8sutils.IsValidCondition(DataPlaneReadyType, oldGwConditionsAware) {
		debug(log, "dataplane is ready", gateway)
	}

	// List Services
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	count := len(services)
	// if too many dataplane services are found here, this is a temporary situation.
	// the number of services will be reduced to 1 by the dataplane controller.
	if count > 1 {
		return ctrl.Result{}, fmt.Errorf("found %d services for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 0 {
		return ctrl.Result{}, fmt.Errorf("no services found for dataplane %s/%s", dataplane.Namespace, dataplane.Name)
	}

	// Provision controlplane creates a controlplane and adds the ControlPlaneReady condition to the Gateway status
	// if the controlplane is ready, the ControlPlaneReady status is set to true, otherwise false
	controlplane := r.provisionControlPlane(ctx, log, gwc.GatewayClass, &gateway, gatewayConfig, dataplane, services)

	// Set the ControlPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no ControlPlaneReady condition in the old gateway, or
	// * the new status is false and the previous status was true
	if !k8sutils.IsValidCondition(ControlPlaneReadyType, gwConditionAware) {
		condition, found := k8sutils.GetCondition(ControlPlaneReadyType, oldGwConditionsAware)
		if !found || condition.Status == metav1.ConditionTrue {
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil {
				return ctrl.Result{}, err
			}
			debug(log, "controlplane not ready yet", gateway)
		}
		return ctrl.Result{}, nil // requeue will be triggered by the update of the controlplane status
	}
	// if the controlplane wasnt't ready before this reconciliation loop and now is ready, log this event
	if !k8sutils.IsValidCondition(ControlPlaneReadyType, oldGwConditionsAware) {
		debug(log, "controlplane is ready", gateway)
	}

	// DataPlane NetworkPolicies
	trace(log, "ensuring DataPlane's NetworkPolicy exists", gateway)
	createdOrUpdated, err := r.ensureDataPlaneHasNetworkPolicy(ctx, &gateway, gatewayConfig, dataplane, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "networkPolicy updated", gateway)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	debug(log, "ensuring DataPlane connectivity for Gateway", gateway)
	connectivityStatusError := r.ensureGatewayConnectivityStatus(ctx, &gateway, dataplane)
	if connectivityStatusError == nil {
		k8sutils.SetCondition(
			k8sutils.NewCondition(GatewayServiceType, metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""),
			gatewayConditionsAware(&gateway),
		)
	} else {
		k8sutils.SetCondition(
			k8sutils.NewCondition(GatewayServiceType, metav1.ConditionFalse, GatewayServiceErrorReason, connectivityStatusError.Error()),
			gatewayConditionsAware(&gateway),
		)
	}

	if !k8sutils.IsReady(gwConditionAware) {
		if !k8sutils.IsReady(oldGwConditionsAware) || !reflect.DeepEqual(gateway.Status.Addresses, oldGateway.Status.Addresses) {
			k8sutils.SetReady(gwConditionAware, gateway.Generation)
			if err = r.patchStatus(ctx, &gateway, oldGateway); err != nil {
				return ctrl.Result{}, err
			}
			debug(log, "gateway is ready", gateway)
		}
	}

	debug(log, "reconciliation complete for Gateway resource", gateway)
	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) provisionDataPlane(
	ctx context.Context,
	log logr.Logger,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
) *operatorv1alpha1.DataPlane {
	log = log.WithName("dataplaneProvisioning")

	r.setDataplaneGatewayConfigDefaults(gatewayConfig)
	trace(log, "looking for associated dataplanes", gateway)
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(
		ctx,
		r.Client,
		gateway,
	)
	if err != nil {
		debug(log, fmt.Sprintf("failed listing associated dataplanes - error: %v", err), gateway)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	count := len(dataplanes)
	if count > 1 {
		err = fmt.Errorf("data plane deployments found: %d, expected: 1", count)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
			gatewayConditionsAware(gateway),
		)
		return nil
	}
	if count == 0 {
		err = r.createDataPlane(ctx, gateway, gatewayConfig)
		if err != nil {
			debug(log, fmt.Sprintf("dataplane creation failed - error: %v", err), gateway)
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
				gatewayConditionsAware(gateway),
			)
		} else {
			debug(log, "dataplane created", gateway)
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage),
				gatewayConditionsAware(gateway),
			)
		}
		return nil
	}
	dataplane := dataplanes[0].DeepCopy()

	trace(log, "ensuring dataplane config is up to date", gateway)
	// compare deployment option of dataplane with dataplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of dataplane with an empty one.
	expectedDataplaneDeploymentOptions := &operatorv1alpha1.DataPlaneDeploymentOptions{}
	if gatewayConfig.Spec.DataPlaneDeploymentOptions != nil {
		expectedDataplaneDeploymentOptions = gatewayConfig.Spec.DataPlaneDeploymentOptions
	}
	if !dataplaneSpecDeepEqual(&dataplane.Spec.DataPlaneDeploymentOptions, expectedDataplaneDeploymentOptions) {
		trace(log, "dataplane config is out of date, updating", gateway)
		dataplane.Spec.DataPlaneDeploymentOptions = *expectedDataplaneDeploymentOptions

		err = r.Client.Update(ctx, dataplane)
		if err != nil {
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
				gatewayConditionsAware(gateway),
			)
			return nil
		}
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage),
			gatewayConditionsAware(gateway),
		)
		debug(log, "dataplane config updated", gateway)
	}

	trace(log, "waiting for dataplane readiness", gateway)

	if k8sutils.IsReady(dataplane) {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""),
			gatewayConditionsAware(gateway),
		)
	} else {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			gatewayConditionsAware(gateway),
		)
	}
	return dataplane
}

func (r *GatewayReconciler) provisionControlPlane(
	ctx context.Context,
	log logr.Logger,
	gatewayClass *gatewayv1beta1.GatewayClass,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplane *operatorv1alpha1.DataPlane,
	services []corev1.Service,
) *operatorv1alpha1.ControlPlane {
	log = log.WithName("controlplaneProvisioning")
	r.setControlplaneGatewayConfigDefaults(gateway, gatewayConfig, dataplane.Name, services[0].Name)

	trace(log, "looking for associated controlplanes", gateway)
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		debug(log, fmt.Sprintf("failed listing associated controlplanes - error: %v", err), gateway)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	count := len(controlplanes)
	if count > 1 {
		err := fmt.Errorf("control plane deployments found: %d, expected: 1, requeing", count)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
			gatewayConditionsAware(gateway),
		)
		return nil
	}
	if count == 0 {
		err := r.createControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane.Name)
		if err != nil {
			debug(log, fmt.Sprintf("controlplane creation failed - error: %v", err), gateway)
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
				gatewayConditionsAware(gateway),
			)
		} else {
			debug(log, "controlplane created", gateway)
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage),
				gatewayConditionsAware(gateway),
			)
		}
		return nil
	}
	controlplane := controlplanes[0].DeepCopy()

	trace(log, "ensuring controlplane config is up to date", gateway)
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		if !controlplaneSpecDeepEqual(&controlplane.Spec.ControlPlaneDeploymentOptions, gatewayConfig.Spec.ControlPlaneDeploymentOptions) {
			trace(log, "controlplane config is out of date, updating", gateway)
			controlplane.Spec.ControlPlaneDeploymentOptions = *gatewayConfig.Spec.ControlPlaneDeploymentOptions
			if err := r.Client.Update(ctx, controlplane); err != nil {
				k8sutils.SetCondition(
					createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
					gatewayConditionsAware(gateway),
				)
				debug(log, fmt.Sprintf("failed updating the controlplane config - error: %v", err), gateway)
				return nil
			}
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage),
				gatewayConditionsAware(gateway),
			)
			debug(log, "controlplane config updated", gateway)
		}
	}
	trace(log, "ensuring controlplane config is up to date", gateway)
	// compare deployment option of controlplane with controlplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of controlplane with an empty one.
	expectedControlplaneDeploymentOptions := &operatorv1alpha1.ControlPlaneDeploymentOptions{}
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		expectedControlplaneDeploymentOptions = gatewayConfig.Spec.ControlPlaneDeploymentOptions
	}

	if !controlplaneSpecDeepEqual(&controlplane.Spec.ControlPlaneDeploymentOptions, expectedControlplaneDeploymentOptions) {
		trace(log, "controlplane config is out of date, updating", gateway)
		controlplane.Spec.ControlPlaneDeploymentOptions = *expectedControlplaneDeploymentOptions
		err = r.Client.Update(ctx, controlplane)
		if err != nil {
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()),
				gatewayConditionsAware(gateway),
			)
			return nil
		}
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage),
			gatewayConditionsAware(gateway),
		)
	}

	trace(log, "waiting for controlplane readiness", gateway)
	if !k8sutils.IsReady(controlplane) {
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	k8sutils.SetCondition(
		createControlPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""),
		gatewayConditionsAware(gateway),
	)
	return controlplane
}

func createDataPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string) metav1.Condition {
	return k8sutils.NewCondition(DataPlaneReadyType, status, reason, message)
}

func createControlPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string) metav1.Condition {
	return k8sutils.NewCondition(ControlPlaneReadyType, status, reason, message)
}

// patchStatus patches the resource status with the Merge strategy
func (r *GatewayReconciler) patchStatus(ctx context.Context, gateway, oldGateway *gwtypes.Gateway) error {
	return r.Client.Status().Patch(ctx, gateway, client.MergeFrom(oldGateway))
}
