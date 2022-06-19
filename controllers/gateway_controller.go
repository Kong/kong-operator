package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

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

	// watch for updates to gatewayclasses, if any gateway classes change, enqueue
	// reconciliation for all supported gateway objects which reference it.
	return c.Watch(
		&source.Kind{Type: &gatewayv1alpha2.GatewayClass{}},
		handler.EnqueueRequestsFromMapFunc(r.listGatewaysForGatewayClass),
		predicate.NewPredicateFuncs(r.gatewayClassMatchesController),
	)
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

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	debug(log, "reconciling gateway resource", req)
	gateway := new(gatewayv1alpha2.Gateway)
	if err := r.Client.Get(ctx, req.NamespacedName, gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "checking class for gateway", gateway)
	gatewayClass, err := r.verifyGatewayClassSupport(ctx, gateway)
	if err != nil {
		if errors.Is(err, ErrUnsupportedGateway) {
			debug(log, "gateway not supported, ignoring", gateway, "ExpectedGatewayClass", vars.ControllerName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "gateway is supported, ensuring that it gets marked as scheduled", gateway)
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

	debug(log, "looking for dataplanes associated with gateway resource", gateway)
	dataplane := new(operatorv1alpha1.DataPlane)
	dataplaneNSN := types.NamespacedName{Namespace: req.Namespace, Name: "dataplane-" + req.Name} // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
	if err := r.Client.Get(ctx, dataplaneNSN, dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, r.createDataPlane(ctx, gateway)
		}
		return ctrl.Result{}, err
	}

	debug(log, "waiting for dataplane for gateway to be ready", gateway)
	dataplaneReady := false
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
			dataplaneReady = true
		}
	}
	if !dataplaneReady {
		debug(log, "dataplane for gateway not ready yet, waiting", gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil
	}

	debug(log, "looking for controlplanes associated with gateway resource", gateway)
	controlplane := new(operatorv1alpha1.ControlPlane)
	controlplaneNSN := types.NamespacedName{Namespace: req.Namespace, Name: "controlplane-" + req.Name} // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
	if err := r.Client.Get(ctx, controlplaneNSN, controlplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, r.createControlPlane(ctx, gatewayClass, gateway, dataplane.Name)
		}
		return ctrl.Result{}, err
	}

	debug(log, "waiting for controlplane for gateway to be ready", gateway)
	controlplaneReady := false
	for _, condition := range controlplane.Status.Conditions {
		if condition.Type == string(ControlPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
			controlplaneReady = true
		}
	}
	if !controlplaneReady {
		debug(log, "controlplane for gateway not ready yet, waiting", gateway)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil
	}

	debug(log, "marking the gateway as ready", gateway)
	if err := r.ensureGatewayMarkedReady(ctx, gateway); err != nil {
		return ctrl.Result{}, err
	}

	debug(log, "gateway successfully reconciled", gateway)
	return ctrl.Result{}, nil
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) createDataPlane(ctx context.Context, gateway *gatewayv1alpha2.Gateway) error {
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "dataplane-" + gateway.Name,
		},
	}
	setObjectOwner(gateway, dataplane)
	labelObjForGateway(dataplane)
	return r.Client.Create(ctx, dataplane)
}

func (r *GatewayReconciler) createControlPlane(ctx context.Context, gatewayClass *gatewayv1alpha2.GatewayClass, gateway *gatewayv1alpha2.Gateway, dataplaneName string) error {
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "controlplane-" + gateway.Name, // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			GatewayClass: (*gatewayv1alpha2.ObjectName)(&gatewayClass.Name),
			DataPlane:    &dataplaneName,
		},
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
