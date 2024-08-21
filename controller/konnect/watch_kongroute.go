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

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/modules/manager/logging"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
					predicate.NewPredicateFuncs(kongRouteRefersToKonnectControlPlane(cl)),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongService{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongRouteForKongService(cl),
				),
			)
		},
	}
}

// kongRouteRefersToKonnectControlPlane returns true if the KongRoute
// refers to a KonnectControlPlane.
func kongRouteRefersToKonnectControlPlane(cl client.Client) func(obj client.Object) bool {
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

		cpRef := kongSvc.Spec.ControlPlaneRef
		return cpRef != nil && cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef
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

		// If the KongService does not refer to a KonnectControlPlane,
		// we do not need to enqueue any KongRoutes bound to this KongService.
		cpRef := kongSvc.Spec.ControlPlaneRef
		if cpRef == nil || cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
			return nil
		}

		var l configurationv1alpha1.KongRouteList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: kongSvc.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, route := range l.Items {
			svcRef, ok := getServiceRef(&route).Get()
			if !ok {
				continue
			}

			switch svcRef.Type {
			case configurationv1alpha1.ServiceRefNamespacedRef:
				if svcRef.NamespacedRef == nil {
					continue
				}

				// TODO: change this when cross namespace refs are allowed.
				if svcRef.NamespacedRef.Name != kongSvc.GetName() {
					continue
				}

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: route.Namespace,
						Name:      route.Name,
					},
				})

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ServiceRef for KongRoute",
					"KongRoute", route, "refType", svcRef.Type,
				)
				continue
			}
		}
		return ret
	}
}
