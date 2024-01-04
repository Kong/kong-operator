package gateway

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/controlplane"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/compare"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler
// -----------------------------------------------------------------------------

// Reconciler reconciles a Gateway object.
type Reconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch Gateway objects, filtering out any Gateways which are not configured with
		// a supported GatewayClass controller name.
		For(&gwtypes.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayHasMatchingGatewayClass))).
		// watch for changes in dataplanes created by the gateway controller
		Owns(&operatorv1beta1.DataPlane{}).
		// watch for changes in controlplanes created by the gateway controller
		Owns(&operatorv1alpha1.ControlPlane{}).
		// watch for changes in networkpolicies created by the gateway controller
		Owns(&networkingv1.NetworkPolicy{}).
		// watch for updates to GatewayConfigurations, if any configuration targets a
		// Gateway that is supported, enqueue that Gateway.
		Watches(
			&operatorv1alpha1.GatewayConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayConfig),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayConfigurationMatchesController))).
		// watch for updates to GatewayClasses, if any GatewayClasses change, enqueue
		// reconciliation for all supported gateway objects which reference it.
		Watches(
			&gatewayv1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayClass),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatchesController))).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "gateway", r.DevelopmentMode)

	log.Trace(logger, "reconciling gateway resource", req)
	var gateway gwtypes.Gateway
	if err := r.Client.Get(ctx, req.NamespacedName, &gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !gateway.DeletionTimestamp.IsZero() {
		if gateway.DeletionTimestamp.After(time.Now()) {
			log.Debug(logger, "gateway deletion still under grace period", gateway)
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Until(gateway.DeletionTimestamp.Time),
			}, nil
		}
		log.Trace(logger, "gateway is marked delete, waiting for owned resources deleted", gateway)

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
				log.Debug(logger, "deleted owned dataplanes", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupDataPlanes)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up dataplanes removed", gateway)
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
				log.Debug(logger, "deleted owned controlplanes", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupControlPlanes)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up controlplanes removed", gateway)
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
				log.Debug(logger, "deleted owned network policies", gateway)
				return ctrl.Result{}, err
			}
		} else {
			oldGateway := gateway.DeepCopy()
			if k8sutils.RemoveFinalizerInMetadata(&gateway.ObjectMeta, string(GatewayFinalizerCleanupNetworkpolicies)) {
				err := r.Client.Patch(ctx, &gateway, client.MergeFrom(oldGateway))
				if err != nil {
					return ctrl.Result{}, err
				}
				log.Debug(logger, "finalizer for cleaning up network policies removed", gateway)
				return ctrl.Result{}, nil
			}
		}

		// cleanup completed
		log.Debug(logger, "owned resource cleanup completed, gateway deleted", gateway)
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	finalizersChanged := k8sutils.EnsureFinalizersInMetadata(&gateway.ObjectMeta,
		string(GatewayFinalizerCleanupControlPlanes),
		string(GatewayFinalizerCleanupDataPlanes),
		string(GatewayFinalizerCleanupNetworkpolicies),
	)
	if finalizersChanged {
		log.Trace(logger, "update metadata of gateway to set finalizer", gateway)
		if err := r.Client.Update(ctx, &gateway); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed updating Gateway's finalizers: %w", err)
		}
		return ctrl.Result{}, nil
	}

	log.Trace(logger, "checking gatewayclass", gateway)
	gwc, err := r.verifyGatewayClassSupport(ctx, &gateway)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrUnsupportedGateway) {
			log.Debug(logger, "resource not supported, ignoring", gateway, "ExpectedGatewayClass", vars.ControllerName())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !gwc.IsAccepted() {
		log.Debug(logger, "gatewayclass not accepted , ignoring", gateway)
		return ctrl.Result{}, nil
	}

	oldGateway := gateway.DeepCopy()
	gwConditionAware := gatewayConditionsAware(&gateway)
	oldGwConditionsAware := gatewayConditionsAware(oldGateway)

	log.Trace(logger, "resource is supported, ensuring that it gets marked as scheduled", gateway)
	if !k8sutils.IsValidCondition(GatewayScheduledType, gwConditionAware) {
		condition := k8sutils.NewConditionWithGeneration(
			k8sutils.ConditionType(gatewayv1.GatewayConditionAccepted),
			metav1.ConditionTrue, k8sutils.ConditionReason(gatewayv1.GatewayClassReasonAccepted),
			fmt.Sprintf("this gateway has been picked up by the %s and will be processed", vars.ControllerName()),
			gateway.Generation,
		)
		k8sutils.SetCondition(condition, gwConditionAware)
	}
	gwConditionAware.InitReadyAndProgrammed()

	log.Trace(logger, "determining configuration", gateway)
	gatewayConfig, err := r.getOrCreateGatewayConfiguration(ctx, gwc.GatewayClass)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Provision dataplane creates a dataplane and adds the DataPlaneReady=True
	// condition to the Gateway status if the dataplane is ready. If not ready
	// the status DataPlaneReady=False will be set instead.
	dataplane := r.provisionDataPlane(ctx, logger, &gateway, gatewayConfig)

	// Set the DataPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no DataPlaneReady condition in the old gateway, or
	// * the new status is false and the previous status was true
	if !k8sutils.IsValidCondition(DataPlaneReadyType, gwConditionAware) {
		condition, found := k8sutils.GetCondition(DataPlaneReadyType, oldGwConditionsAware)
		if !found || condition.Status == metav1.ConditionTrue {
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil { // requeue will be triggered by the update of the dataplane status
				return ctrl.Result{}, err
			}
			log.Debug(logger, "dataplane not ready yet", gateway)
		}
		return ctrl.Result{}, nil
	}
	// if the dataplane wasnt't ready before this reconciliation loop and now is ready, log this event
	if !k8sutils.IsValidCondition(DataPlaneReadyType, oldGwConditionsAware) {
		log.Debug(logger, "dataplane is ready", gateway)
	}

	// List Services
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		},
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
	controlplane := r.provisionControlPlane(ctx, logger, gwc.GatewayClass, &gateway, gatewayConfig, dataplane, services)

	// Set the ControlPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no ControlPlaneReady condition in the old gateway, or
	// * the new status is false and the previous status was true
	if !k8sutils.IsValidCondition(ControlPlaneReadyType, gwConditionAware) {
		condition, found := k8sutils.GetCondition(ControlPlaneReadyType, oldGwConditionsAware)
		if !found || condition.Status == metav1.ConditionTrue {
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "controlplane not ready yet", gateway)
		}
		return ctrl.Result{}, nil // requeue will be triggered by the update of the controlplane status
	}
	// if the controlplane wasnt't ready before this reconciliation loop and now is ready, log this event
	if !k8sutils.IsValidCondition(ControlPlaneReadyType, oldGwConditionsAware) {
		log.Debug(logger, "controlplane is ready", gateway)
	}

	// DataPlane NetworkPolicies
	log.Trace(logger, "ensuring DataPlane's NetworkPolicy exists", gateway)
	createdOrUpdated, err := r.ensureDataPlaneHasNetworkPolicy(ctx, &gateway, gatewayConfig, dataplane, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "networkPolicy updated", gateway)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring DataPlane connectivity for Gateway", gateway)
	gateway.Status.Addresses, err = r.getGatewayAddresses(ctx, dataplane)
	if err == nil {
		k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(GatewayServiceType, metav1.ConditionTrue, k8sutils.ResourceReadyReason, "", gateway.Generation),
			gatewayConditionsAware(&gateway))
	} else {
		log.Info(logger, "could not determine gateway status: %s", err)
		k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(GatewayServiceType, metav1.ConditionFalse, GatewayServiceErrorReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(&gateway))
	}

	if (!k8sutils.IsProgrammed(gwConditionAware) && !k8sutils.IsProgrammed(oldGwConditionsAware)) || !reflect.DeepEqual(gateway.Status.Addresses, oldGateway.Status.Addresses) {
		gwConditionAware.SetReadyAndProgrammed()
		log.Debug(logger, "gateway is Programmed", gateway)
		if err = r.patchStatus(ctx, &gateway, oldGateway); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Debug(logger, "reconciliation complete for Gateway resource", gateway)
	return ctrl.Result{}, nil
}

func (r *Reconciler) provisionDataPlane(
	ctx context.Context,
	logger logr.Logger,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
) *operatorv1beta1.DataPlane {
	logger = logger.WithName("dataplaneProvisioning")

	r.setDataplaneGatewayConfigDefaults(gatewayConfig)
	log.Trace(logger, "looking for associated dataplanes", gateway)
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(
		ctx,
		r.Client,
		gateway,
	)
	if err != nil {
		log.Debug(logger, fmt.Sprintf("failed listing associated dataplanes - error: %v", err), gateway)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	count := len(dataplanes)
	if count > 1 {
		err = fmt.Errorf("data plane deployments found: %d, expected: 1", count)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}
	if count == 0 {
		err = r.createDataPlane(ctx, gateway, gatewayConfig)
		if err != nil {
			log.Debug(logger, fmt.Sprintf("dataplane creation failed - error: %v", err), gateway)
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAware(gateway),
			)
		} else {
			log.Debug(logger, "dataplane created", gateway)
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage, gateway.Generation),
				gatewayConditionsAware(gateway),
			)
		}
		return nil
	}
	dataplane := dataplanes[0].DeepCopy()

	log.Trace(logger, "ensuring dataplane config is up to date", gateway)
	// compare deployment option of dataplane with dataplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of dataplane with an empty one.
	expectedDataplaneOptions := &operatorv1beta1.DataPlaneOptions{}
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		expectedDataplaneOptions = gatewayConfig.Spec.DataPlaneOptions
	}
	// Don't require setting defaults for DataPlane when using Gateway CRD.
	setDataPlaneOptionsDefaults(expectedDataplaneOptions)

	if !dataplaneSpecDeepEqual(&dataplane.Spec.DataPlaneOptions, expectedDataplaneOptions) {
		log.Trace(logger, "dataplane config is out of date, updating", gateway)
		dataplane.Spec.DataPlaneOptions = *expectedDataplaneOptions

		err = r.Client.Update(ctx, dataplane)
		if err != nil {
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAware(gateway),
			)
			return nil
		}
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage, gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		log.Debug(logger, "dataplane config updated", gateway)
	}

	log.Trace(logger, "waiting for dataplane readiness", gateway)

	if k8sutils.IsReady(dataplane) {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, "", gateway.Generation),
			gatewayConditionsAware(gateway),
		)
	} else {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage, gateway.Generation),
			gatewayConditionsAware(gateway),
		)
	}
	return dataplane
}

func (r *Reconciler) provisionControlPlane(
	ctx context.Context,
	logger logr.Logger,
	gatewayClass *gatewayv1.GatewayClass,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplane *operatorv1beta1.DataPlane,
	services []corev1.Service,
) *operatorv1alpha1.ControlPlane {
	logger = logger.WithName("controlplaneProvisioning")
	err := r.setControlplaneGatewayConfigDefaults(gateway, gatewayConfig, dataplane.Name, services[0].Name)
	if err != nil {
		log.Debug(logger, fmt.Sprintf("failed setting the GatewayConfig defaults - error: %v", err), gateway)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	log.Trace(logger, "looking for associated controlplanes", gateway)
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		log.Debug(logger, fmt.Sprintf("failed listing associated controlplanes - error: %v", err), gateway)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	count := len(controlplanes)
	if count > 1 {
		err := fmt.Errorf("control plane deployments found: %d, expected: 1, requeing", count)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}
	if count == 0 {
		err := r.createControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane.Name)
		if err != nil {
			log.Debug(logger, fmt.Sprintf("controlplane creation failed - error: %v", err), gateway)
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAware(gateway),
			)
		} else {
			log.Debug(logger, "controlplane created", gateway)
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceCreatedMessage, gateway.Generation),
				gatewayConditionsAware(gateway),
			)
		}
		return nil
	}
	controlPlane := controlplanes[0].DeepCopy()

	log.Trace(logger, "ensuring controlplane config is up to date", gateway)
	// compare deployment option of controlplane with controlplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of controlplane with an empty one.
	expectedControlplaneOptions := &operatorv1alpha1.ControlPlaneOptions{}
	if gatewayConfig.Spec.ControlPlaneOptions != nil {
		expectedControlplaneOptions = gatewayConfig.Spec.ControlPlaneOptions
	}
	// Don't require setting defaults for ControlPlane when using Gateway CRD.
	setControlPlaneOptionsDefaults(expectedControlplaneOptions)

	if !controlplane.SpecDeepEqual(&controlPlane.Spec.ControlPlaneOptions, expectedControlplaneOptions, "CONTROLLER_KONG_ADMIN_URL") {
		log.Trace(logger, "controlplane config is out of date, updating", gateway)
		controlplaneOld := controlPlane.DeepCopy()
		controlPlane.Spec.ControlPlaneOptions = *expectedControlplaneOptions
		if err := r.Client.Patch(ctx, controlPlane, client.MergeFrom(controlplaneOld)); err != nil {
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, k8sutils.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAware(gateway),
			)
			return nil
		}
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.ResourceCreatedOrUpdatedReason, k8sutils.ResourceUpdatedMessage, gateway.Generation),
			gatewayConditionsAware(gateway),
		)
	}

	log.Trace(logger, "waiting for controlplane readiness", gateway)
	if !k8sutils.IsReady(controlPlane) {
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage, gateway.Generation),
			gatewayConditionsAware(gateway),
		)
		return nil
	}

	k8sutils.SetCondition(
		createControlPlaneCondition(metav1.ConditionTrue, k8sutils.ResourceReadyReason, "", gateway.Generation),
		gatewayConditionsAware(gateway),
	)
	return controlPlane
}

// setControlPlaneOptionsDefaults sets the default ControlPlane options not overriding
// what's been provided only filling in those fields that were unset or empty.
func setControlPlaneOptionsDefaults(opts *operatorv1alpha1.ControlPlaneOptions) {
	if opts.Deployment.PodTemplateSpec == nil {
		opts.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	container := k8sutils.GetPodContainerByName(&opts.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container != nil {
		if container.Image == "" {
			container.Image = consts.DefaultControlPlaneImage
		}
	} else {
		// Because we currently require image to be specified for ControlPlanes
		// we need to add it here. After #20 gets resolved this won't be needed
		// anymore.
		// Related:
		// - https://github.com/Kong/gateway-operator/issues/20
		// - https://github.com/Kong/gateway-operator/issues/754
		opts.Deployment.PodTemplateSpec.Spec.Containers = append(opts.Deployment.PodTemplateSpec.Spec.Containers, corev1.Container{
			Name:  consts.ControlPlaneControllerContainerName,
			Image: consts.DefaultControlPlaneImage,
		})
	}

	if opts.Deployment.Replicas == nil {
		opts.Deployment.Replicas = lo.ToPtr(int32(1))
	}
}

// setDataPlaneOptionsDefaults sets the default DataPlane options not overriding
// what's been provided only filling in those fields that were unset or empty.
func setDataPlaneOptionsDefaults(opts *operatorv1beta1.DataPlaneOptions) {
	if opts.Deployment.PodTemplateSpec == nil {
		opts.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	container := k8sutils.GetPodContainerByName(&opts.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container != nil {
		if container.Image == "" {
			container.Image = consts.DefaultDataPlaneImage
		}
	} else {
		// Because we currently require image to be specified for DataPlanes
		// we need to add it here. After #20 gets resolved this won't be needed
		// anymore.
		// Related:
		// - https://github.com/Kong/gateway-operator/issues/20
		// - https://github.com/Kong/gateway-operator/issues/754
		opts.Deployment.PodTemplateSpec.Spec.Containers = append(opts.Deployment.PodTemplateSpec.Spec.Containers, corev1.Container{
			Name:  consts.DataPlaneProxyContainerName,
			Image: consts.DefaultDataPlaneImage,
		})
	}

	if opts.Deployment.Replicas == nil {
		opts.Deployment.Replicas = lo.ToPtr(int32(1))
	}
}

func createDataPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string, observedGeneration int64) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(DataPlaneReadyType, status, reason, message, observedGeneration)
}

func createControlPlaneCondition(status metav1.ConditionStatus, reason k8sutils.ConditionReason, message string, observedGeneration int64) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(ControlPlaneReadyType, status, reason, message, observedGeneration)
}

// patchStatus patches the resource status with the Merge strategy
func (r *Reconciler) patchStatus(ctx context.Context, gateway, oldGateway *gwtypes.Gateway) error {
	return r.Client.Status().Patch(ctx, gateway, client.MergeFrom(oldGateway))
}

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1beta1.DataPlaneOptions) bool {
	// TODO: Doesn't take .Rollout field into account.
	if !deploymentOptionsDeepEqual(&spec1.Deployment.DeploymentOptions, &spec2.Deployment.DeploymentOptions) ||
		!compare.NetworkOptionsDeepEqual(&spec1.Network, &spec2.Network) {
		return false
	}

	return true
}

func deploymentOptionsDeepEqual(o1, o2 *operatorv1beta1.DeploymentOptions, envVarsToIgnore ...string) bool {
	if o1 == nil && o2 == nil {
		return true
	}

	if (o1 == nil && o2 != nil) || (o1 != nil && o2 == nil) {
		return false
	}

	if !reflect.DeepEqual(o1.Replicas, o2.Replicas) {
		return false
	}

	opts := []cmp.Option{
		cmp.Comparer(func(a, b corev1.ResourceRequirements) bool {
			return k8sresources.ResourceRequirementsEqual(a, b)
		}),
		cmp.Comparer(func(a, b []corev1.EnvVar) bool {
			// Throw out env vars that we ignore.
			a = lo.Filter(a, func(e corev1.EnvVar, _ int) bool {
				return !lo.Contains(envVarsToIgnore, e.Name)
			})
			b = lo.Filter(b, func(e corev1.EnvVar, _ int) bool {
				return !lo.Contains(envVarsToIgnore, e.Name)
			})

			// And compare.
			return reflect.DeepEqual(a, b)
		}),
	}
	return cmp.Equal(&o1.PodTemplateSpec, &o2.PodTemplateSpec, opts...)
}
