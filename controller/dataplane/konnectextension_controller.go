package dataplane

import (
	"context"
	"reflect"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/ctxinjector"
	"github.com/kong/gateway-operator/controller/pkg/log"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
)

// -----------------------------------------------------------------------------
// DataKonnectExtensionReconciler
// -----------------------------------------------------------------------------

// KonnectExtensionReconciler reconciles a KonnectExtension object.
type KonnectExtensionReconciler struct {
	client.Client
	ContextInjector ctxinjector.CtxInjector
	// DevelopmentMode indicates if the controller should run in development mode,
	// which causes it to e.g. perform less validations.
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectExtensionReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	or := reconcile.AsReconciler[*operatorv1alpha1.KonnectExtension](mgr.GetClient(), r)
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.KonnectExtension{}).
		Watches(&operatorv1beta1.DataPlane{}, handler.EnqueueRequestsFromMapFunc(r.listDataPlaneExtensionsReferenced)).
		Complete(or)
}

// listDataPlaneExtensionsReferenced returns a list of all the KonnectExtensions referenced by the DataPlane object.
// Maximum one reference is expected.
func (r *KonnectExtensionReconciler) listDataPlaneExtensionsReferenced(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)
	dataPlane, ok := obj.(*operatorv1beta1.DataPlane)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "DataPlane", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	if len(dataPlane.Spec.Extensions) == 0 {
		return nil
	}

	recs := []reconcile.Request{}

	for _, ext := range dataPlane.Spec.Extensions {
		namespace := dataPlane.Namespace
		if ext.Group != operatorv1alpha1.SchemeGroupVersion.Group ||
			ext.Kind != operatorv1alpha1.KonnectExtensionKind {
			continue
		}
		if ext.Namespace != nil && *ext.Namespace != namespace {
			continue
		}
		recs = append(recs, reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: namespace,
				Name:      ext.Name,
			},
		})
	}
	return recs
}

// Reconcile reconciles a KonnectExtension object.
func (r *KonnectExtensionReconciler) Reconcile(ctx context.Context, konnectExtension *operatorv1alpha1.KonnectExtension) (ctrl.Result, error) {
	ctx = r.ContextInjector.InjectKeyValues(ctx)

	logger := log.GetLogger(ctx, operatorv1alpha1.KonnectExtensionKind, r.DevelopmentMode)
	var dataPlaneList operatorv1beta1.DataPlaneList
	if err := r.List(ctx, &dataPlaneList, client.MatchingFields{
		index.KonnectExtensionIndex: konnectExtension.Namespace + "/" + konnectExtension.Name,
	}); err != nil {
		return ctrl.Result{}, err
	}

	var updated bool
	switch len(dataPlaneList.Items) {
	case 0:
		updated = controllerutil.RemoveFinalizer(konnectExtension, consts.DataPlaneExtensionFinalizer)
	default:
		updated = controllerutil.AddFinalizer(konnectExtension, consts.DataPlaneExtensionFinalizer)
	}
	if updated {
		if err := r.Client.Update(ctx, konnectExtension); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}

		log.Info(logger, "KonnectExtension finalizer updated")
	}

	return ctrl.Result{}, nil
}
