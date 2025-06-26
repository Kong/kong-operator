package konnect

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/internal/utils/index"

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
	credentialSecretLabelSelector, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      SecretCredentialLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create label selector predicate: %v", err))
	}

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
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1.KongConsumerList](
						cl, index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1.KongConsumerList](
						cl, index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
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
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&corev1.Secret{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongConsumerForKongCredentialSecret(cl),
				),
				builder.WithPredicates(
					credentialSecretLabelSelector,
				),
			)
		},
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
			index.IndexFieldKongConsumerOnKongConsumerGroup: group.Name,
		}); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}

func enqueueKongConsumerForKongCredentialSecret(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		s, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		// List consumers using this Secret as credential.
		var l configurationv1.KongConsumerList
		err := cl.List(
			ctx,
			&l,
			client.MatchingFields{
				index.IndexFieldKongConsumerReferencesSecrets: s.GetName(),
			},
		)
		if err != nil {
			return nil
		}

		return objectListToReconcileRequests(
			lo.Filter(l.Items, func(c configurationv1.KongConsumer, _ int) bool {
				return objHasControlPlaneRef(&c)
			}),
		)
	}
}
