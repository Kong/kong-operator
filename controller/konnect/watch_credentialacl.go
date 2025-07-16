package konnect

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/internal/utils/index"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// kongCredentialACLReconciliationWatchOptions returns the watch options for
// the KongCredentialACL resource.
func kongCredentialACLReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongCredentialACL{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(
						kongCredentialRefersToKonnectGatewayControlPlane[*configurationv1alpha1.KongCredentialACL](cl),
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1.KongConsumer{},
				handler.EnqueueRequestsFromMapFunc(
					kongCredentialACLForKongConsumer(cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1.KongConsumer]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					kongCredentialACLForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					kongCredentialACLForKonnectGatewayControlPlane(cl),
				),
			)
		},
	}
}

func kongCredentialACLForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}

		var l configurationv1.KongConsumerList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(auth.GetNamespace()),
		); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, consumer := range l.Items {
			cpRef, ok := controlplane.GetControlPlaneRef(&consumer).Get()
			if !ok {
				continue
			}
			cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, consumer.Namespace)
			if err != nil {
				ctrllog.FromContext(ctx).Error(
					err,
					"failed to get KonnectGatewayControlPlane",
					"KonnectGatewayControlPlane", cpRef,
				)
				continue
			}

			// TODO: change this when cross namespace refs are allowed.
			if cp.GetKonnectAPIAuthConfigurationRef().Name != auth.Name {
				continue
			}

			var credList configurationv1alpha1.KongCredentialACLList
			if err := cl.List(ctx, &credList,
				client.MatchingFields{
					index.IndexFieldKongCredentialACLReferencesKongConsumer: consumer.Name,
				},
				client.InNamespace(auth.GetNamespace()),
			); err != nil {
				return nil
			}

			for _, cred := range credList.Items {
				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: cred.Namespace,
						Name:      cred.Name,
					},
				},
				)
			}
		}
		return ret
	}
}

func kongCredentialACLForKonnectGatewayControlPlane(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}
		var l configurationv1.KongConsumerList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(cp.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKongConsumerOnKonnectGatewayControlPlane: cp.Namespace + "/" + cp.Name,
			},
		); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, consumer := range l.Items {
			var credList configurationv1alpha1.KongCredentialACLList
			if err := cl.List(ctx, &credList,
				client.MatchingFields{
					index.IndexFieldKongCredentialACLReferencesKongConsumer: consumer.Name,
				},
				client.InNamespace(cp.GetNamespace()),
			); err != nil {
				return nil
			}

			for _, cred := range credList.Items {
				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: cred.Namespace,
						Name:      cred.Name,
					},
				},
				)
			}
		}
		return ret
	}
}

func kongCredentialACLForKongConsumer(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		consumer, ok := obj.(*configurationv1.KongConsumer)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongCredentialACLList
		if err := cl.List(ctx, &l,
			client.MatchingFields{
				index.IndexFieldKongCredentialACLReferencesKongConsumer: consumer.Name,
			},
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(consumer.GetNamespace()),
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
