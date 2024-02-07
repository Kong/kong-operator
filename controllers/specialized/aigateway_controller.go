package specialized

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/apis/v1alpha1"
)

// AIGatewayReconciler reconciles a AIGateway object
type AIGatewayReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/finalizers,verbs=update

// Reconcile reconciles the AIGateway resource.
func (r *AIGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.FromContext(ctx).Info(
		"AIGateway found, but controller not yet implemented. Quitting.",
		"namespace", req.Namespace, "name", req.Name,
	)

	// TODO: implement the reconcile workflow
	//
	// High level workflow overview:
	//
	// 1. create an owned Gateway resource (must be v3.6.0 or RC)
	// 2. push all LLM configuration to the AI plugins
	// 3. create a consumer with credentials
	// 3. configure the plugin with the consumer
	// 5. update status with endpoint, consumers auth, e.t.c.

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.AIGateway{}).
		Complete(r)
}
