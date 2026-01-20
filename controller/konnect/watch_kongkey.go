package konnect

import (
	"context"

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

// KongKeyReconciliationWatchOptions returns the watch options for the KongKey.
func KongKeyReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongKey{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongKey]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongKeySet{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongKeyForKongKeySet(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1alpha1.KongKeyList](
						cl, index.IndexFieldKongKeyOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongKeyList](
						cl, index.IndexFieldKongKeyOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongReferenceGrant{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectsForKongReferenceGrant[configurationv1alpha1.KongKeyList](cl),
				),
			)
		},
		// TODO: add watch for KonnectGatewayControlPlane through KeySet reference.
	}
}

func enqueueKongKeyForKongKeySet(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		keySet, ok := obj.(*configurationv1alpha1.KongKeySet)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongKeyList
		if err := cl.List(ctx, &l,
			client.InNamespace(keySet.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKongKeyOnKongKeySetReference: keySet.GetNamespace() + "/" + keySet.GetName(),
			},
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
