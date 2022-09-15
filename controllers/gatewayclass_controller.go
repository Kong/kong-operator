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

	gwc := new(gatewayv1beta1.GatewayClass)
	if err := r.Client.Get(ctx, req.NamespacedName, gwc); err != nil {
		if errors.IsNotFound(err) {
			debug(log, "object enqueued no longer exists, skipping", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	debug(log, "processing gatewayclass", gwc)

	if isGatewayClassControlled(gwc) {
		alreadyAccepted := gatewayClassIsAccepted(gwc)

		if !alreadyAccepted {
			acceptedCondtion := metav1.Condition{
				Type:               string(gatewayv1beta1.GatewayClassConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: gwc.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gatewayv1beta1.GatewayClassReasonAccepted),
				Message:            "the gatewayclass has been accepted by the controller",
			}
			setGatewayClassCondition(gwc, acceptedCondtion)
			return ctrl.Result{}, r.Status().Update(ctx, pruneGatewayClassStatusConds(gwc))
		}
	}

	return ctrl.Result{}, nil
}

func gatewayClassIsAccepted(gwc *gatewayv1beta1.GatewayClass) bool {
	for _, cond := range gwc.Status.Conditions {
		if cond.Reason == string(gatewayv1beta1.GatewayClassConditionStatusAccepted) {
			if cond.ObservedGeneration == gwc.Generation {
				return true
			}
		}
	}
	return false
}
