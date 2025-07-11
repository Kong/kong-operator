package konnect

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorerrors "github.com/kong/kong-operator/internal/errors"
	"github.com/kong/kong-operator/internal/utils/index"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KongRouteReconciliationWatchOptions returns the watch options for
// the KongRoute.
func KongRouteReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongRoute{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(kongRouteRefersToKonnectGatewayControlPlane(cl)),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongService{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongRouteForKongService(cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongService]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongRouteList](
						cl, index.IndexFieldKongRouteOnKonnectGatewayControlPlane,
					),
				),
			)
		},
	}
}

// kongRouteRefersToKonnectGatewayControlPlane returns true if the KongRoute
// refers to a KonnectGatewayControlPlane.
func kongRouteRefersToKonnectGatewayControlPlane(cl client.Client) func(obj client.Object) bool {
	return func(obj client.Object) bool {
		kongRoute, ok := obj.(*configurationv1alpha1.KongRoute)
		if !ok {
			ctrllog.FromContext(context.Background()).Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run predicate function",
				"expected", "KongRoute", "found", reflect.TypeOf(obj),
			)
			return false
		}

		// If the KongRoute refers to a KonnectGatewayControlPlane directly (it's a serviceless route),
		// enqueue it.
		if objHasControlPlaneRef(kongRoute) {
			return true
		}

		scvRef := kongRoute.Spec.ServiceRef
		if scvRef == nil || scvRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
			return false
		}
		nn := types.NamespacedName{
			Namespace: kongRoute.Namespace,
			Name:      scvRef.NamespacedRef.Name,
		}
		kongSvc := configurationv1alpha1.KongService{}
		if err := cl.Get(context.Background(), nn, &kongSvc); client.IgnoreNotFound(err) != nil {
			return true
		}
		return objHasControlPlaneRef(&kongSvc)
	}
}

func enqueueKongRouteForKongService(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongSvc, ok := obj.(*configurationv1alpha1.KongService)
		if !ok {
			return nil
		}

		// If the KongService does not refer to a KonnectGatewayControlPlane,
		// we do not need to enqueue any KongRoutes bound to this KongService.
		if !objHasControlPlaneRef(kongSvc) {
			return nil
		}

		var l configurationv1alpha1.KongRouteList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(kongSvc.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKongRouteOnReferencedKongService: kongSvc.Namespace + "/" + kongSvc.Name,
			},
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
