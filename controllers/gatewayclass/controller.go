package gatewayclass

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controllers/pkg/log"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayClassReconciler
// -----------------------------------------------------------------------------

// GatewayReconciler reconciles a Gateway object
type Reconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatches))).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "gatewayclass", r.DevelopmentMode)

	log.Trace(logger, "reconciling gatewayclass resource", req)

	gwc := NewDecorator()
	if err := r.Client.Get(ctx, req.NamespacedName, gwc.GatewayClass); err != nil {
		if errors.IsNotFound(err) {
			log.Debug(logger, "object enqueued no longer exists, skipping", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.Debug(logger, "processing gatewayclass", gwc)

	if gwc.isControlled() {
		if !gwc.IsAccepted() {
			acceptedCondition := metav1.Condition{
				Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: gwc.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gatewayv1.GatewayClassReasonAccepted),
				Message:            "the gatewayclass has been accepted by the operator",
			}
			k8sutils.SetCondition(acceptedCondition, gwc)
			if err := r.Status().Update(ctx, gwc.GatewayClass); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed updating GatewayClass: %w", err)
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// Decorator struct adds additional functionality to the base K8s Gateway.
type Decorator struct {
	*gatewayv1.GatewayClass
}

// GetConditions returns status conditions of GatewayClass.
func (gwc *Decorator) GetConditions() []metav1.Condition {
	return gwc.Status.Conditions
}

// SetConditions sets status conditions of GatewayClass.
func (gwc *Decorator) SetConditions(conditions []metav1.Condition) {
	gwc.Status.Conditions = conditions
}

// NewDecorator returns Decorator object to add additional functionality to the base K8s GatewayClass.
func NewDecorator() *Decorator {
	return &Decorator{
		new(gatewayv1.GatewayClass),
	}
}

func decorateGatewayClass(gwc *gatewayv1.GatewayClass) *Decorator {
	return &Decorator{GatewayClass: gwc}
}

// IsAccepted returns true if the GatewayClass has been accepted by the operator.
func (gwc *Decorator) IsAccepted() bool {
	if cond, ok := k8sutils.GetCondition(k8sutils.ConditionType(gatewayv1.GatewayClassConditionStatusAccepted), gwc); ok {
		return cond.Reason == string(gatewayv1.GatewayClassReasonAccepted) &&
			cond.ObservedGeneration == gwc.Generation && cond.Status == metav1.ConditionTrue
	}
	return false
}

// isControlled returns boolean if the GatewayClass is controlled by this controller.
func (gwc *Decorator) isControlled() bool {
	return string(gwc.Spec.ControllerName) == vars.ControllerName()
}
