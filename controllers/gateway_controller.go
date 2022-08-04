package controllers

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
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
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("gateway", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// watch for changes in the networkpolicies created by the gateway operator
	if err := c.Watch(
		&source.Kind{Type: &networkingv1.NetworkPolicy{}},
		&handler.EnqueueRequestForOwner{OwnerType: &gatewayv1alpha2.Gateway{}},
	); err != nil {
		return err
	}

	// watch Gateway objects, filtering out any Gateways which are not configured with
	// a supported GatewayClass controller name.
	if err := c.Watch(
		&source.Kind{Type: &gatewayv1alpha2.Gateway{}},
		&handler.EnqueueRequestForObject{},
		predicate.NewPredicateFuncs(r.gatewayHasMatchingGatewayClass),
	); err != nil {
		return err
	}

	// watch for updates to GatewayClasses, if any GatewayClasses change, enqueue
	// reconciliation for all supported gateway objects which reference it.
	if err := c.Watch(
		&source.Kind{Type: &gatewayv1alpha2.GatewayClass{}},
		handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayClass),
		predicate.NewPredicateFuncs(r.gatewayClassMatchesController),
	); err != nil {
		return err
	}

	// watch for updates to GatewayConfigurations, if any configuration targets a
	// Gateway that is supported, enqueue that Gateway.
	return c.Watch(
		&source.Kind{Type: &operatorv1alpha1.GatewayConfiguration{}},
		handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayConfig),
		predicate.NewPredicateFuncs(r.gatewayConfigurationMatchesController),
	)
}

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("gateway")

	debug(log, "reconciling gateway resource", req)
	gateway := newGateway()
	if err := r.Client.Get(ctx, req.NamespacedName, gateway.Gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	k8sutils.InitReady(gateway)

	debug(log, "checking gatewayclass", gateway)
	gatewayClass, err := r.verifyGatewayClassSupport(ctx, gateway.Gateway)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrUnsupportedGateway) {
			debug(log, "resource not supported, ignoring", gateway, "ExpectedGatewayClass", vars.ControllerName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "resource is supported, ensuring that it gets marked as scheduled", gateway)
	if !k8sutils.IsValidCondition(GatewayScheduledType, gateway) {
		condition := k8sutils.NewCondition(
			k8sutils.ConditionType(gatewayv1alpha2.GatewayConditionScheduled),
			metav1.ConditionTrue, k8sutils.ConditionReason(gatewayv1alpha2.GatewayReasonScheduled),
			fmt.Sprintf("this gateway has been picked up by the %s and will be processed", vars.ControllerName),
		)
		k8sutils.SetCondition(condition, gateway)
	}

	debug(log, "determining configuration", gateway)
	gatewayConfig, err := r.getOrCreateGatewayConfiguration(ctx, gatewayClass)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Dataplane
	dataplane := r.provisionDataPlane(ctx, gateway, gatewayConfig)

	if !k8sutils.IsValidCondition(DataPlaneReadyType, gateway) {
		err := r.updateStatus(ctx, gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, err
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
	if count > 1 {
		return ctrl.Result{}, fmt.Errorf("found %d services for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 0 {
		return ctrl.Result{}, fmt.Errorf("no services found for dataplane %s/%s", dataplane.Namespace, dataplane.Name)
	}

	// ControlPlane
	controlplane := r.provisionControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane, services)

	if !k8sutils.IsValidCondition(ControlPlaneReadyType, gateway) {
		err := r.updateStatus(ctx, gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, err
	}

	// DataPlane NetworkPolicies
	debug(log, "ensuring DataPlane's NetworkPolicy is created", gateway)
	if err := r.ensureDataPlaneHasNetworkPolicy(ctx, gateway, dataplane, controlplane); err != nil {
		return ctrl.Result{}, err
	}

	// Mark Gateway Ready
	debug(log, "marking the gateway as ready", gateway)
	err = r.ensureGatewayMarkedReady(ctx, gateway, dataplane)
	if err != nil {
		debug(log, "marking the gateway as not ready", gateway)
		k8sutils.SetCondition(k8sutils.NewCondition(GatewayServiceType, metav1.ConditionFalse, GatewayServiceErrorReason, err.Error()), gateway)
	} else {
		debug(log, "marking the gateway as ready", gateway)
		k8sutils.SetCondition(k8sutils.NewCondition(GatewayServiceType, metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""), gateway)
	}

	if !k8sutils.IsReady(gateway) {
		k8sutils.SetReady(gateway)
	}

	err = r.updateStatus(ctx, gateway)
	if err == nil {
		debug(log, "successfully reconciled", gateway)
	} else {
		debug(log, "error trying to persist status", gateway)
	}
	return ctrl.Result{}, err
}

func (r *GatewayReconciler) provisionDataPlane(ctx context.Context, gateway *gatewayDecorator, gatewayConfig *operatorv1alpha1.GatewayConfiguration) *operatorv1alpha1.DataPlane {
	log := log.FromContext(ctx).WithName("gateway")

	r.setDataplaneGatewayConfigDefaults(gatewayConfig)
	debug(log, "looking for associated dataplanes", gateway)
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(
		ctx,
		r.Client,
		gateway.Gateway,
	)
	if err != nil {
		k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		return nil
	}

	count := len(dataplanes)
	if count > 1 {
		err = fmt.Errorf("data plane deployments found: %d, expected: 1, requeing", count)
		k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		return nil
	}
	if count == 0 {
		err = r.createDataPlane(ctx, gateway, gatewayConfig)
		if err != nil {
			k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		} else {
			k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage), gateway)
		}
		return nil
	}
	dataplane := dataplanes[0].DeepCopy()

	debug(log, "ensuring dataplane config is up to date", gateway)
	if gatewayConfig.Spec.DataPlaneDeploymentOptions != nil {
		if !dataplaneSpecDeepEqual(&dataplane.Spec.DataPlaneDeploymentOptions, gatewayConfig.Spec.DataPlaneDeploymentOptions) {
			debug(log, "dataplane config is out of date, updating", gateway)
			dataplane.Spec.DataPlaneDeploymentOptions = *gatewayConfig.Spec.DataPlaneDeploymentOptions
			err = r.Client.Update(ctx, dataplane)
			if err != nil {
				k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
				return nil
			}
			k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage), gateway)
		}
	}

	debug(log, "waiting for dataplane readiness", gateway)

	if k8sutils.IsReady(dataplane) {
		k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""), gateway)
	} else {
		k8sutils.SetCondition(createDataPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage), gateway)
	}
	return dataplane
}

func (r *GatewayReconciler) provisionControlPlane(
	ctx context.Context,
	gatewayClass *gatewayv1alpha2.GatewayClass,
	gateway *gatewayDecorator,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplane *operatorv1alpha1.DataPlane,
	services []corev1.Service,
) *operatorv1alpha1.ControlPlane {
	log := log.FromContext(ctx).WithName("gateway")

	r.setControlplaneGatewayConfigDefaults(gateway, gatewayConfig, dataplane.Name, services[0].Name)

	debug(log, "looking for associated controlplanes", gateway)
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway.Gateway)
	if err != nil {
		k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		return nil
	}

	count := len(controlplanes)
	if count > 1 {
		err := fmt.Errorf("control plane deployments found: %d, expected: 1, requeing", count)
		k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		return nil
	}
	if count == 0 {
		err := r.createControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane.Name)
		if err != nil {
			k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
		} else {
			k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage), gateway)
		}
		return nil
	}
	controlplane := controlplanes[0].DeepCopy()

	debug(log, "ensuring controlplane config is up to date", gateway)
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		if !controlplaneSpecDeepEqual(&controlplane.Spec.ControlPlaneDeploymentOptions, gatewayConfig.Spec.ControlPlaneDeploymentOptions) {
			debug(log, "controlplane config is out of date, updating", gateway)
			controlplane.Spec.ControlPlaneDeploymentOptions = *gatewayConfig.Spec.ControlPlaneDeploymentOptions
			err = r.Client.Update(ctx, controlplane)
			if err != nil {
				k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error()), gateway)
				return nil
			}
			k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage), gateway)
		}
	}

	debug(log, "waiting for controlplane readiness", gateway)
	if !k8sutils.IsReady(controlplane) {
		k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage), gateway)
		return nil
	}

	k8sutils.SetCondition(createControlPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""), gateway)
	return controlplane

}

func createDataPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string) metav1.Condition {
	return k8sutils.NewCondition(DataPlaneReadyType, status, reason, message)
}

func createControlPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string) metav1.Condition {
	return k8sutils.NewCondition(ControlPlaneReadyType, status, reason, message)
}

// updateStatus Updates the resource status only when there are changes in the Conditions
func (r *GatewayReconciler) updateStatus(ctx context.Context, updated *gatewayDecorator) error {
	current := newGateway()
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated.Gateway), current.Gateway)

	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if k8sutils.NeedsUpdate(current, updated) {
		return r.Client.Status().Update(ctx, updated.Gateway)
	}
	return nil
}
