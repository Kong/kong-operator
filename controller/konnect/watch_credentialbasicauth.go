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

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// CredentialBasicAuthReconciliationWatchOptions returns the watch options for
// the CredentialBasicAuth.
func CredentialBasicAuthReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.CredentialBasicAuth{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(CredentialBasicAuthRefersToKonnectGatewayControlPlane(cl)),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					credentialBasicAuthForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					credentialBasicAuthForKonnectGatewayControlPlane(cl),
				),
			)
		},
	}
}

// CredentialBasicAuthRefersToKonnectGatewayControlPlane returns true if the CredentialBasicAuth
// refers to a KonnectGatewayControlPlane.
func CredentialBasicAuthRefersToKonnectGatewayControlPlane(cl client.Client) func(obj client.Object) bool {
	return func(obj client.Object) bool {
		credentialBasicAuth, ok := obj.(*configurationv1alpha1.CredentialBasicAuth)
		if !ok {
			ctrllog.FromContext(context.Background()).Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run predicate function",
				"expected", "CredentialBasicAuth", "found", reflect.TypeOf(obj),
			)
			return false
		}

		consumerRef := credentialBasicAuth.Spec.ConsumerRef
		nn := types.NamespacedName{
			Namespace: credentialBasicAuth.Namespace,
			Name:      consumerRef.Name,
		}
		consumer := configurationv1.KongConsumer{}
		if err := cl.Get(context.Background(), nn, &consumer); client.IgnoreNotFound(err) != nil {
			return true
		}

		cpRef := consumer.Spec.ControlPlaneRef
		return cpRef != nil && cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef
	}
}

func credentialBasicAuthForKonnectAPIAuthConfiguration(
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
			cpRef := consumer.Spec.ControlPlaneRef
			if cpRef == nil ||
				cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != auth.GetName() {
				continue
			}

			cpNN := types.NamespacedName{
				Name:      cpRef.KonnectNamespacedRef.Name,
				Namespace: consumer.Namespace,
			}
			var cp konnectv1alpha1.KonnectGatewayControlPlane
			if err := cl.Get(ctx, cpNN, &cp); err != nil {
				ctrllog.FromContext(ctx).Error(
					err,
					"failed to get KonnectGatewayControlPlane",
					"KonnectGatewayControlPlane", cpNN,
				)
				continue
			}

			// TODO: change this when cross namespace refs are allowed.
			if cp.GetKonnectAPIAuthConfigurationRef().Name != auth.Name {
				continue
			}

			var credList configurationv1alpha1.CredentialBasicAuthList
			if err := cl.List(ctx, &credList,
				client.MatchingFields{
					IndexFieldCredentialBasicAuthReferencesKongConsumer: consumer.Name,
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

func credentialBasicAuthForKonnectGatewayControlPlane(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}
		var l configurationv1.KongConsumerList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(cp.GetNamespace()),
		); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, consumer := range l.Items {
			cpRef := consumer.Spec.ControlPlaneRef
			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp.GetName() {
				continue
			}

			var credList configurationv1alpha1.CredentialBasicAuthList
			if err := cl.List(ctx, &credList,
				client.MatchingFields{
					IndexFieldCredentialBasicAuthReferencesKongConsumer: consumer.Name,
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
