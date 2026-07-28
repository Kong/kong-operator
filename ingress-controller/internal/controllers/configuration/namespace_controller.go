package configuration

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/logging"
)

// -----------------------------------------------------------------------------
// CoreV1 Namespace - Reconciler
// -----------------------------------------------------------------------------

// CoreV1NamespaceReconciler reconciles Namespace resources.
//
// Namespaces are cached so that the translator can evaluate a Gateway
// listener's AllowedRoutes.Namespaces From: Selector the same way the
// gateway controller does (both need the referenced Namespace's labels).
type CoreV1NamespaceReconciler struct {
	client.Client

	Log              logr.Logger
	Scheme           *runtime.Scheme
	DataplaneClient  controllers.DataPlane
	CacheSyncTimeout time.Duration
}

var _ controllers.Reconciler = &CoreV1NamespaceReconciler{}

// SetupWithManager sets up the controller with the Manager.
func (r *CoreV1NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("CoreV1Namespace").
		WithOptions(controller.Options{
			LogConstructor: func(_ *reconcile.Request) logr.Logger {
				return r.Log
			},
			CacheSyncTimeout: r.CacheSyncTimeout,
		}).
		For(&corev1.Namespace{}).
		Complete(r)
}

// SetLogger sets the logger.
func (r *CoreV1NamespaceReconciler) SetLogger(l logr.Logger) {
	r.Log = l
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile processes the watched objects.
func (r *CoreV1NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("CoreV1Namespace", req.NamespacedName)

	namespace := new(corev1.Namespace)
	if err := r.Get(ctx, req.NamespacedName, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			namespace.Name = req.Name
			return ctrl.Result{}, r.DataplaneClient.DeleteObject(namespace)
		}
		return ctrl.Result{}, err
	}

	log.V(logging.DebugLevel).Info("Reconciling Namespace resource", "name", req.Name)

	if err := r.DataplaneClient.UpdateObject(namespace); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
