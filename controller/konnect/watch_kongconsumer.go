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

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KongConsumerReconciliationWatchOptions returns the watch options for
// the KongConsumer.
func KongConsumerReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1.KongConsumer{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1.KongConsumer]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongConsumerForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1.KongConsumerList](
						cl, IndexFieldKongConsumerOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1beta1.KongConsumerGroup{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongConsumerForKongConsumerGroup(cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1beta1.KongConsumerGroup]),
				),
			)
		},
	}
}

func enqueueKongConsumerForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l configurationv1.KongConsumerList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: auth.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, consumer := range l.Items {
			cpRef, ok := getControlPlaneRef(&consumer).Get()
			if !ok {
				continue
			}

			cp, err := getCPForRef(ctx, cl, cpRef, consumer.GetNamespace())
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

			ret = append(ret, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: consumer.Namespace,
					Name:      consumer.Name,
				},
			})
		}

		return ret
	}
}

func enqueueKongConsumerForKongConsumerGroup(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		group, ok := obj.(*configurationv1beta1.KongConsumerGroup)
		if !ok {
			return nil
		}
		var l configurationv1.KongConsumerList
		if err := cl.List(ctx, &l, client.MatchingFields{
			IndexFieldKongConsumerOnKongConsumerGroup: group.Name,
		}); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
