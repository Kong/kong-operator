package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/kong/go-kong/kong"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha2.Gateway{}).
		Named("Gateway").
		Complete(r)
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciliation
// -----------------------------------------------------------------------------

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=create;get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=create;get;list;watch;update;patch

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	debug(log, "reconciling gateway resource", req)
	gateway := new(gatewayv1alpha2.Gateway)
	if err := r.Client.Get(ctx, req.NamespacedName, gateway); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "looking for dataplanes associated with gateway resource", gateway)
	dataplane := new(operatorv1alpha1.DataPlane)
	dataplaneNSN := types.NamespacedName{Namespace: req.Namespace, Name: "dataplane-" + req.Name} // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
	if err := r.Client.Get(ctx, dataplaneNSN, dataplane); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, r.CreateDataPlane(ctx, gateway)
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
		if errors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, r.CreateControlPlane(ctx, gateway, dataplane.Name)
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

func (r *GatewayReconciler) CreateDataPlane(ctx context.Context, gateway *gatewayv1alpha2.Gateway) error {
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "dataplane-" + gateway.Name,
		},
	}
	setObjectOwner(gateway, dataplane)
	return r.Client.Create(ctx, dataplane)
}

func (r *GatewayReconciler) CreateControlPlane(ctx context.Context, gateway *gatewayv1alpha2.Gateway, dataplaneName string) error {
	gatewayClassName := gatewayv1alpha2.ObjectName("kong") // TODO: gwc support https://github.com/Kong/gateway-operator/issues/22

	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gateway.Namespace,
			Name:      "controlplane-" + gateway.Name, // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			GatewayClass: &gatewayClassName,
			DataPlane:    kong.String(dataplaneName), // TODO: ctrl runtime swap https://github.com/Kong/gateway-operator/issues/23
		},
	}
	setObjectOwner(gateway, controlplane)
	return r.Client.Create(ctx, controlplane)
}

func (r *GatewayReconciler) ensureGatewayMarkedReady(ctx context.Context, gateway *gatewayv1alpha2.Gateway) error {
	ready := false
	for _, condition := range gateway.Status.Conditions {
		if condition.Type == string(gatewayv1alpha2.GatewayConditionReady) {
			ready = true
		}
	}

	if !ready {
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

		newConditions := make([]metav1.Condition, 0, len(gateway.Status.Conditions))
		if len(gateway.Status.Conditions) >= 8 {
			newConditions = gateway.Status.Conditions[:7]
		}
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
