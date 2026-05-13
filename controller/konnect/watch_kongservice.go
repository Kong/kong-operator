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

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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
				// Only re-enqueue the KongService when the KongRoute spec changes
				// (Create / Delete / generation-bumping Update). Status-only updates
				// must NOT trigger KongService reconciliation: without this predicate,
				// every KongRoute status patch causes a KongService reconcile, whose
				// own status update then re-triggers KongRoute reconciliation, forming
				// an infinite loop.
				builder.WithPredicates(predicate.GenerationChangedPredicate{}),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongCertificate{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongServiceForKongCertificate(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongCACertificate{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongServiceForKongCACertificate(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongReferenceGrant{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectsForKongReferenceGrant[configurationv1alpha1.KongServiceList](cl),
				),
			)
		},
	}
}

// enqueueKongServiceForKongCertificate returns a mapper that re-enqueues all
// KongServices that reference the changed KongCertificate via spec.clientCertificateRef.
func enqueueKongServiceForKongCertificate(
	cl client.Client,
) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cert, ok := obj.(*configurationv1alpha1.KongCertificate)
		if !ok {
			return nil
		}
		svcList := configurationv1alpha1.KongServiceList{}
		if err := cl.List(ctx, &svcList,
			client.MatchingFields{
				index.IndexFieldKongServiceOnReferencedKongCertificate: cert.Namespace + "/" + cert.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(svcList.Items)
	}
}

// enqueueKongServiceForKongCACertificate returns a mapper that re-enqueues all
// KongServices that reference the changed KongCACertificate via spec.caCertificateRefs.
func enqueueKongServiceForKongCACertificate(
	cl client.Client,
) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cacert, ok := obj.(*configurationv1alpha1.KongCACertificate)
		if !ok {
			return nil
		}
		svcList := configurationv1alpha1.KongServiceList{}
		if err := cl.List(ctx, &svcList,
			client.MatchingFields{
				index.IndexFieldKongServiceOnReferencedKongCACertificates: cacert.Namespace + "/" + cacert.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(svcList.Items)
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
