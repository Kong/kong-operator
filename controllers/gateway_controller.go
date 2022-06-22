package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - RBAC Permissions
// -----------------------------------------------------------------------------

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=create;get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=create;get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=gatewayconfigurations,verbs=get;list;watch

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

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("gateway")

	debug(log, "reconciling gateway resource", req)
	gateway := new(gatewayv1alpha2.Gateway)
	if err := r.Client.Get(ctx, req.NamespacedName, gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "checking gatewayclass", gateway)
	gatewayClass, err := r.verifyGatewayClassSupport(ctx, gateway)
	if err != nil {
		if errors.Is(err, ErrUnsupportedGateway) {
			debug(log, "resource not supported, ignoring", gateway, "ExpectedGatewayClass", vars.ControllerName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "resource is supported, ensuring that it gets marked as scheduled", gateway)
	if !gatewayutils.IsGatewayScheduled(gateway) {
		gateway.Status.Conditions = append(gateway.Status.Conditions, metav1.Condition{
			Type:               string(gatewayv1alpha2.GatewayConditionScheduled),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: gateway.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             string(gatewayv1alpha2.GatewayReasonScheduled),
			Message:            fmt.Sprintf("this gateway has been picked up by the %s and will be processed", vars.ControllerName),
		})
		return ctrl.Result{}, r.Status().Update(ctx, pruneGatewayStatusConds(gateway))
	}

	debug(log, "determining configuration", gateway)
	gatewayConfig, err := r.getOrCreateGatewayConfiguration(ctx, gatewayClass)
	if err != nil {
		return ctrl.Result{}, err
	}
	r.setGatewayConfigDefaults(gateway, gatewayConfig)

	debug(log, "looking for associated dataplanes", gateway)
	dataplane := new(operatorv1alpha1.DataPlane)
	dataplaneNSN := types.NamespacedName{Namespace: req.Namespace, Name: "dataplane-" + req.Name} // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
	if err := r.Client.Get(ctx, dataplaneNSN, dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, r.createDataPlane(ctx, gateway, gatewayConfig)
		}
		return ctrl.Result{}, err
	}

	debug(log, "ensuring dataplane config is up to date", gateway)
	if gatewayConfig.Spec.DataPlaneDeploymentOptions != nil {
		if !dataplaneSpecDeepEqual(&dataplane.Spec.DataPlaneDeploymentOptions, gatewayConfig.Spec.DataPlaneDeploymentOptions) {
			debug(log, "dataplane config is out of date, updating", gateway)
			dataplane.Spec.DataPlaneDeploymentOptions = *gatewayConfig.Spec.DataPlaneDeploymentOptions
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, r.Client.Update(ctx, dataplane)
		}
	}

	debug(log, "waiting for dataplane readiness", gateway)
	dataplaneReady := false
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
			dataplaneReady = true
		}
	}
	if !dataplaneReady {
		debug(log, "dataplane not ready yet, waiting", gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "looking for associated controlplanes", gateway)
	controlplane := new(operatorv1alpha1.ControlPlane)
	controlplaneNSN := types.NamespacedName{Namespace: req.Namespace, Name: "controlplane-" + req.Name} // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
	if err := r.Client.Get(ctx, controlplaneNSN, controlplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, r.createControlPlane(ctx, gatewayClass, gateway, gatewayConfig, dataplane.Name)
		}
		return ctrl.Result{}, err
	}

	debug(log, "ensuring controlplane config is up to date", gateway)
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		if !controlplaneSpecDeepEqual(&controlplane.Spec.ControlPlaneDeploymentOptions, gatewayConfig.Spec.ControlPlaneDeploymentOptions) {
			debug(log, "controlplane config is out of date, updating", gateway)
			controlplane.Spec.ControlPlaneDeploymentOptions = *gatewayConfig.Spec.ControlPlaneDeploymentOptions
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, r.Client.Update(ctx, controlplane)
		}
	}

	debug(log, "waiting for controlplane readiness", gateway)
	controlplaneReady := false
	for _, condition := range controlplane.Status.Conditions {
		if condition.Type == string(ControlPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
			controlplaneReady = true
		}
	}
	if !controlplaneReady {
		debug(log, "controlplane not ready yet, waiting", gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "marking the gateway as ready", gateway)
	if err := r.ensureGatewayMarkedReady(ctx, gateway); err != nil {
		return ctrl.Result{}, err
	}

	debug(log, "successfully reconciled", gateway)
	return ctrl.Result{}, nil
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) createDataPlane(ctx context.Context, gateway *gatewayv1alpha2.Gateway, gatewayConfig *operatorv1alpha1.GatewayConfiguration) error {
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "dataplane-" + gateway.Name,
		},
	}
	if gatewayConfig.Spec.DataPlaneDeploymentOptions != nil {
		dataplane.Spec.DataPlaneDeploymentOptions = *gatewayConfig.Spec.DataPlaneDeploymentOptions
	}
	setObjectOwner(gateway, dataplane)
	labelObjForGateway(dataplane)
	return r.Client.Create(ctx, dataplane)
}

func (r *GatewayReconciler) createControlPlane(
	ctx context.Context,
	gatewayClass *gatewayv1alpha2.GatewayClass,
	gateway *gatewayv1alpha2.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplaneName string,
) error {
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "controlplane-" + gateway.Name, // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
				DataPlane: &dataplaneName,
			},
			GatewayClass: (*gatewayv1alpha2.ObjectName)(&gatewayClass.Name),
		},
	}
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		controlplane.Spec.ControlPlaneDeploymentOptions = *gatewayConfig.Spec.ControlPlaneDeploymentOptions
	}
	setObjectOwner(gateway, controlplane)
	labelObjForGateway(controlplane)
	return r.Client.Create(ctx, controlplane)
}

func (r *GatewayReconciler) ensureGatewayMarkedReady(ctx context.Context, gateway *gatewayv1alpha2.Gateway) error {
	if !gatewayutils.IsGatewayReady(gateway) {
		svc := new(corev1.Service)
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: fmt.Sprintf("svc-dataplane-%s", gateway.Name)}, svc); err != nil {
			return err
		}

		if svc.Spec.ClusterIP == "" {
			return fmt.Errorf("service %s doesn't have a ClusterIP yet, not ready", svc.Name)
		}

		gatewayIPs := make([]string, 0)
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			gatewayIPs = append(gatewayIPs, svc.Status.LoadBalancer.Ingress[0].IP) // TODO: handle hostnames https://github.com/Kong/gateway-operator/issues/24
		}

		newAddresses := make([]gatewayv1alpha2.GatewayAddress, 0, len(gatewayIPs))
		ipaddrT := gatewayv1alpha2.IPAddressType
		for _, ip := range append(gatewayIPs, svc.Spec.ClusterIP) {
			newAddresses = append(newAddresses, gatewayv1alpha2.GatewayAddress{
				Type:  &ipaddrT,
				Value: ip,
			})
		}

		gateway.Status.Addresses = newAddresses

		gateway = pruneGatewayStatusConds(gateway)
		newConditions := make([]metav1.Condition, 0, len(gateway.Status.Conditions))
		newConditions = append(newConditions, metav1.Condition{
			Type:               string(gatewayv1alpha2.GatewayConditionReady),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: gateway.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             string(gatewayv1alpha2.GatewayReasonReady),
		})
		gateway.Status.Conditions = newConditions
		return r.Client.Status().Update(ctx, gateway)
	}

	return nil
}

func (r *GatewayReconciler) verifyGatewayClassSupport(ctx context.Context, gateway *gatewayv1alpha2.Gateway) (*gatewayv1alpha2.GatewayClass, error) {
	if gateway.Spec.GatewayClassName == "" {
		return nil, ErrUnsupportedGateway
	}

	gwc := new(gatewayv1alpha2.GatewayClass)
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, gwc); err != nil {
		return nil, err
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName {
		return nil, ErrUnsupportedGateway
	}

	return gwc, nil
}

func (r *GatewayReconciler) getOrCreateGatewayConfiguration(ctx context.Context, gatewayClass *gatewayv1alpha2.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
	if gatewayClass.Spec.ParametersRef == nil {
		return new(operatorv1alpha1.GatewayConfiguration), nil
	}
	return r.getGatewayConfigForGatewayClass(ctx, gatewayClass)
}

func (r *GatewayReconciler) getGatewayConfigForGatewayClass(ctx context.Context, gatewayClass *gatewayv1alpha2.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
	if string(gatewayClass.Spec.ParametersRef.Group) != operatorv1alpha1.GroupVersion.Group ||
		string(gatewayClass.Spec.ParametersRef.Kind) != "GatewayConfiguration" {
		return nil, &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Status: metav1.StatusFailure,
				Code:   http.StatusBadRequest,
				Reason: metav1.StatusReasonInvalid,
				Details: &metav1.StatusDetails{
					Kind: string(gatewayClass.Spec.ParametersRef.Kind),
					Causes: []metav1.StatusCause{{
						Type: metav1.CauseTypeFieldValueNotSupported,
						Message: fmt.Sprintf("controller only supports %s %s resources for GatewayClass parametersRef",
							operatorv1alpha1.GroupVersion.Group, "GatewayConfiguration"),
					}},
				},
			}}
	}

	gatewayConfig := new(operatorv1alpha1.GatewayConfiguration)
	return gatewayConfig, r.Client.Get(ctx, client.ObjectKey{
		Namespace: string(*gatewayClass.Spec.ParametersRef.Namespace),
		Name:      gatewayClass.Spec.ParametersRef.Name,
	}, gatewayConfig)
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Predicates
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) gatewayHasMatchingGatewayClass(obj client.Object) bool {
	gateway, ok := obj.(*gatewayv1alpha2.Gateway)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run predicate function",
			"expected", "Gateway", "found", reflect.TypeOf(obj),
		)
		return false
	}

	_, err := r.verifyGatewayClassSupport(context.Background(), gateway)
	if err != nil {
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return !errors.Is(err, ErrUnsupportedGateway)
	}

	return true
}

func (r *GatewayReconciler) gatewayClassMatchesController(obj client.Object) bool {
	gatewayClass, ok := obj.(*gatewayv1alpha2.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return string(gatewayClass.Spec.ControllerName) == vars.ControllerName
}

func (r *GatewayReconciler) gatewayConfigurationMatchesController(obj client.Object) bool {
	ctx := context.Background()

	gatewayClassList := new(gatewayv1alpha2.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return true
	}

	for _, gatewayClass := range gatewayClassList.Items {
		if string(gatewayClass.Spec.ControllerName) == vars.ControllerName {
			return true
		}
	}

	return false
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Map Funcs
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) listGatewaysForGatewayClass(obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1alpha2.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1alpha2.GatewayList)
	if err := r.Client.List(context.Background(), gateways); err != nil {
		log.FromContext(context.Background()).Error(err, "could not list gateways in map func")
		return
	}

	for _, gateway := range gateways.Items {
		if gateway.Spec.GatewayClassName == gatewayv1alpha2.ObjectName(gatewayClass.Name) {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gateway.Namespace,
					Name:      gateway.Name,
				},
			})
		}
	}

	return
}

func (r *GatewayReconciler) listGatewaysForGatewayConfig(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	gatewayConfig, ok := obj.(*operatorv1alpha1.GatewayConfiguration)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run map funcs",
			"expected", "GatewayConfiguration", "found", reflect.TypeOf(obj),
		)
		return
	}

	gatewayClassList := new(gatewayv1alpha2.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing GatewayClass resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return
	}

	matchingGatewayClasses := make(map[string]struct{})
	for _, gatewayClass := range gatewayClassList.Items {
		if gatewayClass.Spec.ParametersRef != nil &&
			string(gatewayClass.Spec.ParametersRef.Group) == operatorv1alpha1.GroupVersion.Group &&
			string(gatewayClass.Spec.ParametersRef.Kind) == "GatewayConfiguration" &&
			gatewayClass.Spec.ParametersRef.Name == gatewayConfig.Name {
			matchingGatewayClasses[gatewayClass.Name] = struct{}{}
		}
	}

	gatewayList := new(gatewayv1alpha2.GatewayList)
	if err := r.Client.List(ctx, gatewayList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing Gateway resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return
	}

	for _, gateway := range gatewayList.Items {
		if _, ok := matchingGatewayClasses[string(gateway.Spec.GatewayClassName)]; ok {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gateway.Namespace,
					Name:      gateway.Name,
				},
			})
		}
	}

	return
}

func (r *GatewayReconciler) setGatewayConfigDefaults(gateway *gatewayv1alpha2.Gateway, gatewayConfig *operatorv1alpha1.GatewayConfiguration) {
	{
		dontOverride := make(map[string]struct{})
		if gatewayConfig.Spec.DataPlaneDeploymentOptions == nil {
			gatewayConfig.Spec.DataPlaneDeploymentOptions = new(operatorv1alpha1.DataPlaneDeploymentOptions)
		}
		for _, env := range gatewayConfig.Spec.DataPlaneDeploymentOptions.Env {
			dontOverride[env.Name] = struct{}{}
		}

		setDataPlaneDefaults(gatewayConfig.Spec.DataPlaneDeploymentOptions, dontOverride)
	}

	{
		dontOverride := make(map[string]struct{})
		if gatewayConfig.Spec.ControlPlaneDeploymentOptions == nil {
			gatewayConfig.Spec.ControlPlaneDeploymentOptions = new(operatorv1alpha1.ControlPlaneDeploymentOptions)
		}
		for _, env := range gatewayConfig.Spec.ControlPlaneDeploymentOptions.Env {
			dontOverride[env.Name] = struct{}{}
		}

		if gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane == nil ||
			*gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane == "" {
			// TODO: generated names https://github.com/Kong/gateway-operator/issues/21
			dataplaneName := fmt.Sprintf("dataplane-%s", gateway.Name)
			gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane = &dataplaneName
		}

		setControlPlaneDefaults(gatewayConfig.Spec.ControlPlaneDeploymentOptions, gateway.Namespace, dontOverride)
	}
}
