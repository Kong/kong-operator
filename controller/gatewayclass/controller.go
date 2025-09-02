package gatewayclass

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/modules/manager/logging"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// -----------------------------------------------------------------------------
// GatewayClassReconciler
// -----------------------------------------------------------------------------

// Reconciler reconciles a Gateway object
type Reconciler struct {
	client.Client
	Scheme                        *runtime.Scheme
	LoggingMode                   logging.Mode
	GatewayAPIExperimentalEnabled bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{},
			builder.WithPredicates(predicate.NewPredicateFuncs(r.gatewayClassMatches))).
		// watch for updates to GatewayConfigurations, if any configuration is
		// referenced by a GatewayClass that matches our controller name then enqueue it.
		Watches(
			&operatorv1beta1.GatewayConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.listGatewayClassesForGatewayConfig)).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "gatewayclass", r.LoggingMode)

	log.Trace(logger, "reconciling gatewayclass resource")

	gwc := gatewayclass.NewDecorator()
	if err := r.Get(ctx, req.NamespacedName, gwc.GatewayClass); err != nil {
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

	// SupportedFeatures is a Gateway API experimental feature, hence it is enforced only
	// when the Gateway API experimental flag is enabled.
	if r.GatewayAPIExperimentalEnabled {
		if condition.Status == metav1.ConditionTrue {
			gatewayConfig, err := getGatewayConfiguration(ctx, r.Client, gwc.GatewayClass)
			// The error here should never be NotFound, as the GatewayClass is accepted (which means the parametersRef has been properly resolved).
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to get GatewayConfiguration: %w", err)
			}
			if err = setSupportedFeatures(ctx, r.Client, gwc.GatewayClass, gatewayConfig); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to set supported features: %w", err)
			}
		}
	}

	if err := r.Client.Status().Patch(ctx, gwc.GatewayClass, client.MergeFrom(oldGwc)); err != nil {
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
