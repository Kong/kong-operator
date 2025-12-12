package konnect

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/internal/utils/index"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KongServiceReconciliationWatchOptions returns the watch options for
// the KongService.
func KongServiceReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongService{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongService]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1alpha1.KongServiceList](
						cl, index.IndexFieldKongServiceOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongServiceList](
						cl, index.IndexFieldKongServiceOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongRoute{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongServiceForKongRoute(),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongReferenceGrant{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongServiceForKongReferenceGrant(cl),
				),
			)
		},
	}
}

// enqueueKongServiceForKongRoute returns a function that enqueues
// a reconcile.Request for the KongService referenced by the KongRoute.
// This is useful for situations like KongRoute deletion where we need
// to reconcile the KongService to unblock its deletion (Konnect API will block
// service deletion that has routes referencing it).
func enqueueKongServiceForKongRoute() func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongRoute, ok := obj.(*configurationv1alpha1.KongRoute)
		if !ok {
			return nil
		}

		serviceRef, ok := getServiceRef(kongRoute).Get()
		if !ok {
			return nil
		}

		if serviceRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
			return nil
		}

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: kongRoute.Namespace,
					Name:      serviceRef.NamespacedRef.Name,
				},
			},
		}
	}
}

// enqueueKongServiceForKongReferenceGrant returns a function that enqueues
// reconcile.Requests for KongServices that are allowed by the KongReferenceGrant.
// This is useful for situations where a KongReferenceGrant is created/updated/deleted
// and we need to reconcile the KongServices that are affected by it.
func enqueueKongServiceForKongReferenceGrant(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		krg, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
		if !ok {
			return nil
		}

		var fromNamespaces []string
		for _, from := range krg.Spec.From {
			if string(from.Group) != configurationv1alpha1.GroupVersion.Group {
				continue
			}
			if string(from.Kind) != "KongService" {
				continue
			}

			fromNamespaces = append(fromNamespaces, string(from.Namespace))
		}

		if len(fromNamespaces) == 0 {
			return nil
		}

		var ret []reconcile.Request
		for _, ns := range fromNamespaces {
			var kongServiceList configurationv1alpha1.KongServiceList
			if err := cl.List(ctx, &kongServiceList, client.InNamespace(ns)); err != nil {
				continue
			}
			for _, ks := range kongServiceList.Items {
				// Note: we only care about KongServices that reference
				// KonnectGatewayControlPlane but also those that do not.
				// This is to ensure that we reconcile all KongServices: those that
				// stopped referencing KonnectGatewayControlPlane will need to have its
				// status conditions updated accordingly.

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: ks.Namespace,
						Name:      ks.Name,
					},
				},
				)
			}
		}

		return ret
	}
}
