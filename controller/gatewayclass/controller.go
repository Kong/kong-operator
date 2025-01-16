package gatewayclass

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
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
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatches))).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "gatewayclass", r.DevelopmentMode)

	log.Trace(logger, "reconciling gatewayclass resource")

	gwc := gatewayclass.NewDecorator()
	if err := r.Client.Get(ctx, req.NamespacedName, gwc.GatewayClass); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Debug(logger, "processing gatewayclass")

	if !gwc.IsControlled() {
		return ctrl.Result{}, nil
	}

	oldGwc := gwc.DeepCopy()

	condition, err := getAcceptedCondition(ctx, r.Client, gwc.GatewayClass)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get accepted condition: %w", err)
	}
	k8sutils.SetCondition(*condition, gwc)

	if err := r.Status().Patch(ctx, gwc.GatewayClass, client.MergeFrom(oldGwc)); err != nil {
		if k8serrors.IsConflict(err) {
			log.Debug(logger, "conflict found when updating GatewayClass, retrying")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: controller.RequeueWithoutBackoff,
			}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed patching GatewayClass: %w", err)
	}

	return ctrl.Result{}, nil
}
