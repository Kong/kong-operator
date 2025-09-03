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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	controlplanecontroller "github.com/kong/gateway-operator/controller/pkg/controlplane"
	"github.com/kong/gateway-operator/controller/pkg/extensions"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/controller/pkg/secrets/ref"
	"github.com/kong/gateway-operator/controller/pkg/watch"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/modules/manager/logging"
	"github.com/kong/gateway-operator/pkg/consts"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/compare"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	kcfgdataplane "github.com/kong/kubernetes-configuration/api/gateway-operator/dataplane"
	kcfggateway "github.com/kong/kubernetes-configuration/api/gateway-operator/gateway"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// -----------------------------------------------------------------------------
// GatewayReconciler
// -----------------------------------------------------------------------------

// Reconciler reconciles a Gateway object.
type Reconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	DefaultDataPlaneImage   string
	KonnectEnabled          bool
	AnonymousReportsEnabled bool
	LoggingMode             logging.Mode
}

// provisionDataPlaneFailRequeueAfter is the time duration after which we retry provisioning
// of managed `DataPlane` when reconciling a `Gateway`.
const provisionDataPlaneFailRetryAfter = 5 * time.Second

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		// watch Gateway objects, filtering out any Gateways which are not configured with
		// a supported GatewayClass controller name.
		For(&gwtypes.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayHasMatchingGatewayClass))).
		// watch for changes in dataplanes created by the gateway controller
		Owns(&operatorv1beta1.DataPlane{}).
		// watch for changes in controlplanes created by the gateway controller
		Owns(&operatorv1beta1.ControlPlane{}).
		// watch for changes in networkpolicies created by the gateway controller
		Owns(&networkingv1.NetworkPolicy{}).
		// watch for updates to GatewayConfigurations, if any configuration targets a
		// Gateway that is supported, enqueue that Gateway.
		Watches(
			&operatorv1beta1.GatewayConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayConfig),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayConfigurationMatchesController))).
		// watch for updates to GatewayClasses, if any GatewayClasses change, enqueue
		// reconciliation for all supported gateway objects which reference it.
		Watches(
			&gatewayv1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayClass),
			builder.WithPredicates(predicate.NewPredicateFuncs(watch.GatewayClassMatchesController))).
		// watch for events on ReferenceGrants, if any ReferenceGrant event happen, enqueue
		// reconciliation for all supported gateway objects that are referenced in a "from"
		// instance.
		Watches(
			&gatewayv1beta1.ReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(r.listReferenceGrantsForGateway),
			builder.WithPredicates(ref.ReferenceGrantForSecretFrom(gatewayv1.GroupName, gatewayv1beta1.Kind("Gateway")))).
		// watch HTTPRoutes so that Gateway listener status can be updated.
		Watches(
			&gatewayv1beta1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewaysAttachedByHTTPRoute)).
		// watch Namespaces so that managed routes have correct status reflected in Gateway's
		// status in status.listeners.attachedRoutes
		// This is required to properly support Gateway's listeners.allowedRoutes.namespaces.selector.
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.listManagedGatewaysInNamespace))

	if r.KonnectEnabled {
		// Watch for changes in KonnectExtension objects that are referenced by GatewayConfigurations used by Gateways objects.
		// They may trigger reconciliation of DataPlane resources.
		builder.WatchesRawSource(
			source.Kind(
				mgr.GetCache(),
				&konnectv1alpha1.KonnectExtension{},
				handler.TypedEnqueueRequestsFromMapFunc(r.listGatewaysForKonnectExtension),
			),
		)
	}
	return builder.Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "gateway", r.LoggingMode)

	log.Trace(logger, "reconciling gateway resource")
	var gateway gwtypes.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gateway); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Trace(logger, "managing cleanup for gateway resource")
	if shouldReturnEarly, result, err := r.cleanup(ctx, logger, &gateway); err != nil || !result.IsZero() {
		return result, err
	} else if shouldReturnEarly {
		return ctrl.Result{}, nil
	}

	log.Trace(logger, "checking gatewayclass")
	gwc, err := gatewayclass.Get(ctx, r.Client, string(gateway.Spec.GatewayClassName))
	if err != nil {
		switch {
		case errors.As(err, &operatorerrors.ErrUnsupportedGatewayClass{}):
			log.Debug(logger, "resource not supported, ignoring",
				"expectedGatewayClass", vars.ControllerName(),
				"gatewayClass", gateway.Spec.GatewayClassName,
				"reason", err.Error(),
			)
			return ctrl.Result{}, nil
		case errors.As(err, &operatorerrors.ErrNotAcceptedGatewayClass{}):
			log.Debug(logger, "GatewayClass not accepted, ignoring",
				"gatewayClass", gateway.Spec.GatewayClassName,
				"reason", err.Error(),
			)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Trace(logger, "managing the gateway resource finalizers")
	cpFinalizerSet := controllerutil.AddFinalizer(&gateway, string(GatewayFinalizerCleanupControlPlanes))
	dpFinalizerSet := controllerutil.AddFinalizer(&gateway, string(GatewayFinalizerCleanupDataPlanes))
	npFinalizerSet := controllerutil.AddFinalizer(&gateway, string(GatewayFinalizerCleanupNetworkPolicies))
	if cpFinalizerSet || dpFinalizerSet || npFinalizerSet {
		log.Trace(logger, "Setting finalizers")
		if err := r.Update(ctx, &gateway); err != nil {
			res, err := handleGatewayFinalizerPatchOrUpdateError(err, logger)
			if err != nil {
				return res, fmt.Errorf("failed updating Gateway's finalizers: %w", err)
			}
			if !res.IsZero() {
				return res, nil
			}
		}
		return ctrl.Result{}, nil
	}

	if !gwc.IsAccepted() {
		log.Debug(logger, "gatewayclass not accepted , ignoring")
		return ctrl.Result{}, nil
	}

	oldGateway := gateway.DeepCopy()
	gwConditionAware := gatewayConditionsAndListenersAware(&gateway)
	oldGwConditionsAware := gatewayConditionsAndListenersAware(oldGateway)

	log.Trace(logger, "resource is supported that it gets marked as accepted")
	gwConditionAware.initListenersStatus()
	gwConditionAware.setConflicted()
	if err = gwConditionAware.setAcceptedAndAttachedRoutes(ctx, r.Client); err != nil {
		return ctrl.Result{}, err
	}

	gwConditionAware.initProgrammedAndListenersStatus()
	if err := gwConditionAware.setResolvedRefsAndSupportedKinds(ctx, r.Client); err != nil {
		return ctrl.Result{}, err
	}
	acceptedCondition, _ := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayConditionAccepted), gwConditionAware)
	// If the static Gateway API conditions (Accepted, ResolvedRefs, Conflicted) changed, we need to update the Gateway status
	if gatewayStatusNeedsUpdate(oldGwConditionsAware, gwConditionAware) {
		// Requeue will be triggered by the update of the gateway status.
		if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, &gateway, oldGateway); err != nil {
			return ctrl.Result{}, err
		}
		if acceptedCondition.Status == metav1.ConditionTrue {
			log.Info(logger, "gateway accepted")
		} else {
			log.Info(logger, "gateway not accepted")
		}
		return ctrl.Result{}, nil
	}
	// If the Gateway is not accepted, do not move on in the reconciliation logic.
	if acceptedCondition.Status == metav1.ConditionFalse {
		// TODO: clean up Dataplane and Controlplane https://github.com/Kong/gateway-operator/issues/126
		return ctrl.Result{}, nil
	}

	log.Trace(logger, "determining configuration")
	gatewayConfig, err := r.getOrCreateGatewayConfiguration(ctx, gwc.GatewayClass)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Provision dataplane creates a dataplane and adds the DataPlaneReady=True
	// condition to the Gateway status if the dataplane is ready. If not ready
	// the status DataPlaneReady=False will be set instead.
	dataplane, provisionErr := r.provisionDataPlane(ctx, logger, &gateway, gatewayConfig)

	// Set the DataPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no DataPlaneReady condition in the old gateway, or
	// * the new status is false and the previous status was true
	// * dataplane provisioning has failed
	// We want to continue the reconciliation loop in case the DataPlane is not ready
	// with the WaitingToBecomeReadyReason statuc condition because when using
	// Kong Gateway's readiness status /status/ready we need to provision the
	// ControlPlane as well to make DataPlane ready.
	if c, ok := k8sutils.GetCondition(kcfggateway.DataPlaneReadyType, gwConditionAware); !ok || c.Status == metav1.ConditionFalse || provisionErr != nil {
		if provisionErr != nil {
			log.Error(logger, provisionErr, "failed to provision dataplane")
		}

		oldCondition, oldFound := k8sutils.GetCondition(kcfggateway.DataPlaneReadyType, oldGwConditionsAware)
		if !oldFound || oldCondition.Status == metav1.ConditionTrue {
			// requeue will be triggered by the update of the dataplane status
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "dataplane not ready yet")
			return ctrl.Result{}, nil
		}

		if dataplane == nil {
			// Having the dataplane==nil here is a corner-case that can be happening sometimes,
			// in case the dataplane provisioning has had some errors, the dataplane ReadyCondition
			// has already been patched with the ConditionFalse, and a new reconciliation loop is triggered.
			log.Debug(logger,
				fmt.Sprintf(
					"dataplane is not ready yet, and the dataplane ready condition has already been set in the gateway, requeue after %s",
					provisionDataPlaneFailRetryAfter,
				),
			)
			return ctrl.Result{RequeueAfter: provisionDataPlaneFailRetryAfter}, nil
		}

		// If the dataplane is not ready yet we requeue.
		// We don't requeue when the DataPlane provisioning has succeeded and
		// DataPlane is waiting to become ready because we need to provision the ControlPlane
		// to send the config to /status/ready endpoint.
		if !ok || c.Reason != string(kcfgdataplane.WaitingToBecomeReadyReason) {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// This should never happen as the dataplane at this point is always != nil.
	// Nevertheless, this kind of check makes the Gateway controller bulletproof.
	if dataplane == nil {
		return ctrl.Result{}, errors.New("unexpected error (dataplane is nil), returning to avoid panic")
	}

	// List ingress Services
	ingressServices, err := k8sutils.ListServicesForOwner(
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

	count := len(ingressServices)
	// if too many dataplane services are found here, this is a temporary situation.
	// the number of services will be reduced to 1 by the dataplane controller.
	if count > 1 {
		log.Info(logger,
			fmt.Sprintf("found %d ingress services found for dataplane, requeuing...", count),
			"dataplane", client.ObjectKeyFromObject(dataplane),
		)
		return ctrl.Result{Requeue: true}, nil
	}
	if count == 0 {
		log.Info(logger,
			"no ingress services found for dataplane",
			"dataplane", client.ObjectKeyFromObject(dataplane),
		)
		return ctrl.Result{Requeue: true}, nil
	}

	// List admin Services
	adminServices, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	count = len(adminServices)
	// If too many dataplane services are found here, this is a temporary situation.
	// The number of services will be reduced to 1 by the dataplane controller.
	if count > 1 {
		log.Info(logger,
			fmt.Sprintf("found %d admin services found for dataplane, requeuing...", count),
			"dataplane", client.ObjectKeyFromObject(dataplane),
		)
		return ctrl.Result{Requeue: true}, nil
	}
	if count == 0 {
		log.Info(logger,
			"no admin services found for dataplane",
			"dataplane", client.ObjectKeyFromObject(dataplane),
		)
		return ctrl.Result{Requeue: true}, nil
	}

	// Provision controlplane creates a controlplane and adds the ControlPlaneReady condition to the Gateway status
	// if the controlplane is ready, the ControlPlaneReady status is set to true, otherwise false.
	controlplane := r.provisionControlPlane(ctx, logger, gwc.GatewayClass, &gateway, gatewayConfig, dataplane, ingressServices[0], adminServices[0])
	// Set the ControlPlaneReady Condition to False. This happens only if:
	// * the new status is false and there was no ControlPlaneReady condition in the gateway
	// * the new status is false and the previous status was true
	if condition, found := k8sutils.GetCondition(kcfggateway.ControlPlaneReadyType, gwConditionAware); found && condition.Status != metav1.ConditionTrue {
		if condition.Reason == string(kcfgdataplane.UnableToProvisionReason) {
			log.Debug(logger, "unable to provision controlplane, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}

		conditionOld, foundOld := k8sutils.GetCondition(kcfggateway.ControlPlaneReadyType, oldGwConditionsAware)
		if !foundOld || conditionOld.Status == metav1.ConditionTrue {
			if err := r.patchStatus(ctx, &gateway, oldGateway); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "controlplane not ready yet")
		}
		return ctrl.Result{}, nil // requeue will be triggered by the update of the controlplane status
	}

	// if the controlplane wasn't ready before this reconciliation loop and now is ready, log this event
	if !k8sutils.HasConditionTrue(kcfggateway.ControlPlaneReadyType, oldGwConditionsAware) {
		log.Debug(logger, "controlplane is ready")
	}
	// If the dataplane has not been marked as ready yet, return and wait for the next reconciliation loop.
	if !k8sutils.HasConditionTrue(kcfggateway.DataPlaneReadyType, gwConditionAware) {
		log.Debug(logger, "controlplane is ready, but dataplane is not ready yet")
		return ctrl.Result{}, nil
	}
	// This should never happen as the controlplane at this point is always != nil.
	// Nevertheless, this kind of check makes the Gateway controller bulletproof.
	if controlplane == nil {
		return ctrl.Result{}, errors.New("unexpected error, controlplane is nil. Returning to avoid panic")
	}

	// DataPlane NetworkPolicies
	log.Trace(logger, "ensuring DataPlane's NetworkPolicy exists")
	createdOrUpdated, err := r.ensureDataPlaneHasNetworkPolicy(ctx, &gateway, dataplane, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "networkPolicy updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring DataPlane connectivity for Gateway")
	gateway.Status.Addresses, err = r.getGatewayAddresses(ctx, dataplane)
	if err == nil {
		k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(kcfggateway.GatewayServiceType, metav1.ConditionTrue, kcfgdataplane.ResourceReadyReason, "", gateway.Generation),
			gatewayConditionsAndListenersAware(&gateway))
	} else {
		k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(kcfggateway.GatewayServiceType, metav1.ConditionFalse, kcfggateway.GatewayReasonServiceError, err.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(&gateway))
	}

	gwConditionAware.setProgrammed()
	res, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, &gateway, oldGateway)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		return ctrl.Result{}, nil // gateway patch will trigger new reconciliation loop
	}

	if k8sutils.IsProgrammed(gwConditionAware) && !k8sutils.IsProgrammed(oldGwConditionsAware) {
		log.Debug(logger, "gateway is Programmed")
	}

	log.Debug(logger, "reconciliation complete for Gateway resource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) provisionDataPlane(
	ctx context.Context,
	logger logr.Logger,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1beta1.GatewayConfiguration,
) (*operatorv1beta1.DataPlane, error) {
	logger = logger.WithName("dataplaneProvisioning")

	r.setDataPlaneGatewayConfigDefaults(gatewayConfig)
	log.Trace(logger, "looking for associated dataplanes")
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(
		ctx,
		r.Client,
		gateway,
	)
	if err != nil {
		errWrap := fmt.Errorf("failed listing associated dataplanes - error: %w", err)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, errWrap.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return nil, errWrap
	}
	count := len(dataplanes)
	if count > 1 {
		err = fmt.Errorf("data planes found: %d, expected: 1", count)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		log.Debug(logger, "reducing dataplanes", "count", count)
		rErr := k8sreduce.ReduceDataPlanes(ctx, r.Client, dataplanes)
		if rErr != nil {
			return nil, fmt.Errorf("failed reducing data planes: %w", rErr)
		}
		return nil, err
	}
	if count == 0 {
		dataplane, err := r.createDataPlane(ctx, gateway, gatewayConfig)
		if err != nil {
			errWrap := fmt.Errorf("dataplane creation failed - error: %w", err)
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, errWrap.Error(), gateway.Generation),
				gatewayConditionsAndListenersAware(gateway),
			)
			return nil, err
		}
		log.Debug(logger, "dataplane created")
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.ResourceCreatedOrUpdatedReason, kcfgdataplane.ResourceCreatedMessage, gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return dataplane, nil
	}
	dataplane := dataplanes[0].DeepCopy()

	log.Trace(logger, "ensuring dataplane config is up to date")
	// compare deployment option of dataplane with dataplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of dataplane with an empty one.
	expectedDataPlaneOptions := &operatorv1beta1.DataPlaneOptions{}
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		expectedDataPlaneOptions = gatewayConfigDataPlaneOptionsToDataPlaneOptions(gatewayConfig.Namespace, *gatewayConfig.Spec.DataPlaneOptions)
	}
	// Don't require setting defaults for DataPlane when using Gateway CRD.
	setDataPlaneOptionsDefaults(expectedDataPlaneOptions, r.DefaultDataPlaneImage)
	err = setDataPlaneIngressServicePorts(expectedDataPlaneOptions, gateway.Spec.Listeners, gatewayConfig.Spec.ListenersOptions)
	if err != nil {
		errWrap := fmt.Errorf("dataplane creation failed - error: %w", err)
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, errWrap.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return nil, errWrap
	}

	expectedDataPlaneOptions.Extensions = extensions.MergeExtensions(gatewayConfig.Spec.Extensions, expectedDataPlaneOptions.Extensions)

	if !dataplaneSpecDeepEqual(&dataplane.Spec.DataPlaneOptions, expectedDataPlaneOptions) {
		log.Trace(logger, "dataplane config is out of date")
		oldDataPlane := dataplane.DeepCopy()
		dataplane.Spec.DataPlaneOptions = *expectedDataPlaneOptions

		if err = r.Patch(ctx, dataplane, client.MergeFrom(oldDataPlane)); err != nil {
			k8sutils.SetCondition(
				createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAndListenersAware(gateway),
			)
			return nil, fmt.Errorf("failed patching the dataplane %s: %w", dataplane.Name, err)
		}
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.ResourceCreatedOrUpdatedReason, kcfgdataplane.ResourceUpdatedMessage, gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		log.Debug(logger, "dataplane config updated")
	}

	log.Trace(logger, "waiting for dataplane readiness")

	if k8sutils.IsReady(dataplane) {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionTrue, kcfgdataplane.ResourceReadyReason, "", gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
	} else {
		k8sutils.SetCondition(
			createDataPlaneCondition(metav1.ConditionFalse, kcfgdataplane.WaitingToBecomeReadyReason, kcfgdataplane.WaitingToBecomeReadyMessage, gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
	}
	return dataplane, nil
}

func (r *Reconciler) provisionControlPlane(
	ctx context.Context,
	logger logr.Logger,
	gatewayClass *gatewayv1.GatewayClass,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1beta1.GatewayConfiguration,
	dataplane *operatorv1beta1.DataPlane,
	ingressService corev1.Service,
	adminService corev1.Service,
) *operatorv1beta1.ControlPlane {
	logger = logger.WithName("controlplaneProvisioning")

	log.Trace(logger, "looking for associated controlplanes")
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		log.Debug(logger, fmt.Sprintf("failed listing associated controlplanes - error: %v", err))
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return nil
	}

	var controlPlane *operatorv1beta1.ControlPlane

	count := len(controlplanes)
	switch {
	case count == 0:
		r.setControlPlaneGatewayConfigDefaults(gateway, gatewayConfig, dataplane.Name, ingressService.Name, adminService.Name, "")
		err := r.createControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane.Name)
		if err != nil {
			log.Debug(logger, fmt.Sprintf("controlplane creation failed - error: %v", err))
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAndListenersAware(gateway),
			)
		} else {
			log.Debug(logger, "controlplane created")
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.ResourceCreatedOrUpdatedReason, kcfgdataplane.ResourceCreatedMessage, gateway.Generation),
				gatewayConditionsAndListenersAware(gateway),
			)
		}
		return nil
	case count > 1:
		err := fmt.Errorf("control plane deployments found: %d, expected: 1, requeing", count)
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return nil
	}

	// If we continue, there is only one controlplane.
	controlPlane = controlplanes[0].DeepCopy()
	r.setControlPlaneGatewayConfigDefaults(gateway, gatewayConfig, dataplane.Name, ingressService.Name, adminService.Name, controlPlane.Name)

	log.Trace(logger, "ensuring controlplane config is up to date")
	// compare deployment option of controlplane with controlplane deployment option of gatewayconfiguration.
	// if not configured in gatewayconfiguration, compare deployment option of controlplane with an empty one.
	expectedControlPlaneOptions := &operatorv1beta1.ControlPlaneOptions{}
	if gatewayConfig.Spec.ControlPlaneOptions != nil {
		expectedControlPlaneOptions = gatewayConfig.Spec.ControlPlaneOptions
	}
	// Don't require setting defaults for ControlPlane when using Gateway CRD.
	setControlPlaneOptionsDefaults(expectedControlPlaneOptions)

	expectedControlPlaneOptions.Extensions = extensions.MergeExtensions(gatewayConfig.Spec.Extensions, expectedControlPlaneOptions.Extensions)

	if !controlplanecontroller.SpecDeepEqual(&controlPlane.Spec.ControlPlaneOptions, expectedControlPlaneOptions) {
		log.Trace(logger, "controlplane config is out of date")
		controlplaneOld := controlPlane.DeepCopy()
		controlPlane.Spec.ControlPlaneOptions = *expectedControlPlaneOptions
		if err := r.Patch(ctx, controlPlane, client.MergeFrom(controlplaneOld)); err != nil {
			k8sutils.SetCondition(
				createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.UnableToProvisionReason, err.Error(), gateway.Generation),
				gatewayConditionsAndListenersAware(gateway),
			)
			return nil
		}
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.ResourceCreatedOrUpdatedReason, kcfgdataplane.ResourceUpdatedMessage, gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
	}

	log.Trace(logger, "waiting for controlplane readiness")
	if !k8sutils.IsReady(controlPlane) {
		k8sutils.SetCondition(
			createControlPlaneCondition(metav1.ConditionFalse, kcfgdataplane.WaitingToBecomeReadyReason, kcfgdataplane.WaitingToBecomeReadyMessage, gateway.Generation),
			gatewayConditionsAndListenersAware(gateway),
		)
		return nil
	}

	k8sutils.SetCondition(
		createControlPlaneCondition(metav1.ConditionTrue, kcfgdataplane.ResourceReadyReason, "", gateway.Generation),
		gatewayConditionsAndListenersAware(gateway),
	)
	return controlPlane
}

// setControlPlaneOptionsDefaults sets the default ControlPlane options not overriding
// what's been provided only filling in those fields that were unset or empty.
func setControlPlaneOptionsDefaults(opts *operatorv1beta1.ControlPlaneOptions) {
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
func setDataPlaneOptionsDefaults(opts *operatorv1beta1.DataPlaneOptions, defaultImage string) {
	if opts.Deployment.PodTemplateSpec == nil {
		opts.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	container := k8sutils.GetPodContainerByName(&opts.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container != nil {
		if container.Image == "" {
			container.Image = defaultImage
		}
		probe := k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint)
		cProbe := container.ReadinessProbe
		if cProbe == nil {
			// For Gateway we set DataPlane's readiness probe to /status/ready so that
			// it's only marked ready when it receives the configuration from the ControlPlane.
			container.ReadinessProbe = probe
		} else if cProbe.HTTPGet == nil && cProbe.Exec == nil && cProbe.TCPSocket == nil && cProbe.GRPC == nil {
			// If user specified custom readiness probe settings (e.g. initial delay, timeout, etc)
			// but has not specified the actual probe, then we ensure that the default HTTPGet probe is used.
			container.ReadinessProbe.HTTPGet = probe.HTTPGet
		}
	} else {
		// Because we currently require image to be specified for DataPlanes
		// we need to add it here. After #20 gets resolved this won't be needed
		// anymore.
		// Related:
		// - https://github.com/Kong/gateway-operator/issues/20
		// - https://github.com/Kong/gateway-operator/issues/754
		opts.Deployment.PodTemplateSpec.Spec.Containers = append(opts.Deployment.PodTemplateSpec.Spec.Containers, corev1.Container{
			Name:           consts.DataPlaneProxyContainerName,
			Image:          defaultImage,
			ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
		})
	}

	// If no replicas are set, set it to default 1, but only if Scaling is not set as well.
	if opts.Deployment.Replicas == nil && opts.Deployment.Scaling == nil {
		opts.Deployment.Replicas = lo.ToPtr(int32(1))
	}
}

func createDataPlaneCondition(status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string, observedGeneration int64) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(kcfggateway.DataPlaneReadyType, status, reason, message, observedGeneration)
}

func createControlPlaneCondition(status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string, observedGeneration int64) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(kcfggateway.ControlPlaneReadyType, status, reason, message, observedGeneration)
}

// patchStatus patches the resource status with the Merge strategy
func (r *Reconciler) patchStatus(ctx context.Context, gateway, oldGateway *gwtypes.Gateway) error {
	return r.Client.Status().Patch(ctx, gateway, client.MergeFrom(oldGateway))
}

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1beta1.DataPlaneOptions) bool {
	// TODO: Doesn't take .Rollout field into account.
	return deploymentOptionsDeepEqual(&spec1.Deployment.DeploymentOptions, &spec2.Deployment.DeploymentOptions) &&
		compare.NetworkOptionsDeepEqual(&spec1.Network, &spec2.Network) &&
		compare.DataPlaneResourceOptionsDeepEqual(&spec1.Resources, &spec2.Resources) &&
		reflect.DeepEqual(spec1.Extensions, spec2.Extensions)
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
		cmp.Comparer(k8sresources.ResourceRequirementsEqual),
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
