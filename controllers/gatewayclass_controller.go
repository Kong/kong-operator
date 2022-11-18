package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayClassReconciler
// -----------------------------------------------------------------------------

// GatewayReconciler reconciles a Gateway object
type GatewayClassReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1beta1.GatewayClass{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatches))).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := getLogger(ctx, "gatewayclass", r.DevelopmentMode)

	trace(log, "reconciling gatewayclass resource", req)

	gwc := newGatewayClass()
	if err := r.Client.Get(ctx, req.NamespacedName, gwc.GatewayClass); err != nil {
		if errors.IsNotFound(err) {
			debug(log, "object enqueued no longer exists, skipping", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	debug(log, "processing gatewayclass", gwc)

	if gwc.isControlled() {
		if !gwc.isAccepted() {
			acceptedCondition := metav1.Condition{
				Type:               string(gatewayv1beta1.GatewayClassConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: gwc.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gatewayv1beta1.GatewayClassReasonAccepted),
				Message:            "the gatewayclass has been accepted by the operator",
			}
			k8sutils.SetCondition(acceptedCondition, gwc)
			return ctrl.Result{}, r.Status().Update(ctx, gwc.GatewayClass)
		}
	}

	return ctrl.Result{}, nil
}

// gatewayDecorator Decorator object to add additional functionality to the base k8s Gateway
type gatewayClassDecorator struct {
	*gatewayv1beta1.GatewayClass
}

func (gwc *gatewayClassDecorator) GetConditions() []metav1.Condition {
	return gwc.Status.Conditions
}

func (gwc *gatewayClassDecorator) SetConditions(conditions []metav1.Condition) {
	gwc.Status.Conditions = conditions
}

func newGatewayClass() *gatewayClassDecorator {
	return &gatewayClassDecorator{
		new(gatewayv1beta1.GatewayClass),
	}
}

func decorateGatewayClass(gwc *gatewayv1beta1.GatewayClass) *gatewayClassDecorator {
	return &gatewayClassDecorator{GatewayClass: gwc}
}

func (gwc *gatewayClassDecorator) isAccepted() bool {
	if cond, ok := k8sutils.GetCondition(k8sutils.ConditionType(gatewayv1beta1.GatewayClassConditionStatusAccepted), gwc); ok {
		return cond.Reason == string(gatewayv1beta1.GatewayClassReasonAccepted) &&
			cond.ObservedGeneration == gwc.Generation && cond.Status == metav1.ConditionTrue
	}
	return false
}

// isControlled returns boolean if the GatewayClass is controlled by this controller.
func (gwc *gatewayClassDecorator) isControlled() bool {
	return string(gwc.Spec.ControllerName) == vars.ControllerName()
}
