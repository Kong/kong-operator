package konnect

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	operatorerrors "github.com/kong/kong-operator/internal/errors"
	"github.com/kong/kong-operator/internal/utils/index"
)

// listKonnectAPIAuthConfigurationsReferencingSecret returns a function that lists
// KonnectAPIAuthConfiguration resources that reference the given Secret.
// This function is intended to be used as a handler for the watch on Secrets.
// NOTE: The Secret has to have the konnect.konghq.com/credential=konnect set
// so that we can efficiently watch only the relevant Secrets' changes.
func listKonnectAPIAuthConfigurationsReferencingSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		secret, ok := obj.(*corev1.Secret)
		if !ok {
			logger.Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run map funcs",
				"expected", "Secret", "found", reflect.TypeOf(obj),
			)
			return nil
		}

		var konnectAPIAuthConfigList konnectv1alpha1.KonnectAPIAuthConfigurationList
		if err := cl.List(ctx, &konnectAPIAuthConfigList); err != nil {
			logger.Error(
				fmt.Errorf("unexpected error occurred while listing KonnectAPIAuthConfiguration resources"),
				"failed to run map funcs",
				"error", err.Error(),
			)
			return nil
		}

		var recs []reconcile.Request
		for _, apiAuth := range konnectAPIAuthConfigList.Items {
			if apiAuth.Spec.Type != konnectv1alpha1.KonnectAPIAuthTypeSecretRef {
				continue
			}

			if apiAuth.Spec.SecretRef == nil ||
				apiAuth.Spec.SecretRef.Name != secret.Name {
				continue
			}

			if (apiAuth.Spec.SecretRef.Namespace != "" && apiAuth.Spec.SecretRef.Namespace != secret.Namespace) ||
				(apiAuth.Spec.SecretRef.Namespace == "" && secret.Namespace != apiAuth.Namespace) {
				continue
			}

			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: apiAuth.Namespace,
					Name:      apiAuth.Name,
				},
			})
		}
		return recs
	}
}

// konnectAPIAuthReferencingTypes is a list of all entity types that can reference
// a KonnectAPIAuthConfiguration. This list is used to set up watches so that when
// a KonnectAPIAuthConfiguration changes, all entities referencing it are reconciled.
var konnectAPIAuthReferencingTypes = []constraints.EntityWithKonnectAPIAuthConfigurationRef{
	&konnectv1alpha1.KonnectCloudGatewayNetwork{},
	&konnectv1alpha2.KonnectGatewayControlPlane{},
	&konnectv1alpha2.KonnectExtension{},
}

// konnectAPIAuthReferencingTypeListsWithIndexes is a map of Konnect resource list types
// to their corresponding index field names that reference KonnectAPIAuth configurations.
// This map is used to identify which resource types need to be reconciled when a
// KonnectAPIAuth object changes, enabling efficient lookup of dependent resources
// through indexed fields.
var konnectAPIAuthReferencingTypeListsWithIndexes = map[client.ObjectList]string{
	&konnectv1alpha1.KonnectCloudGatewayNetworkList{}: index.IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration,
	&konnectv1alpha2.KonnectGatewayControlPlaneList{}: index.IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration,
	&konnectv1alpha2.KonnectExtensionList{}:           index.IndexFieldKonnectExtensionOnAPIAuthConfiguration,
}

// setKonnectAPIAuthConfigurationRefWatches sets up watches for types that reference KonnectAPIAuthConfiguration.
func setKonnectAPIAuthConfigurationRefWatches(b *builder.Builder) *builder.Builder {
	for _, t := range konnectAPIAuthReferencingTypes {
		b = b.Watches(
			t,
			handler.EnqueueRequestsFromMapFunc(
				listKonnectAPIAuthConfigurationsRefByEntity[constraints.EntityWithKonnectAPIAuthConfigurationRef](),
			),
		)
	}

	return b
}

// listKonnectAPIAuthConfigurationsRefByEntity returns a watch handler that maps
// entities with KonnectAPIAuthConfiguration references to reconcile requests.
// The returned handler extracts the auth configuration reference from the entity
// and creates a reconcile request for it in the entity's namespace.
func listKonnectAPIAuthConfigurationsRefByEntity[T constraints.EntityWithKonnectAPIAuthConfigurationRef]() func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		tEnt, ok := obj.(T)
		if !ok {
			logger.Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run map funcs",
				"expected", "KonnectGatewayControlPlane", "found", reflect.TypeOf(obj),
			)
			return nil
		}

		namespace := tEnt.GetNamespace()
		if tEnt.GetKonnectAPIAuthConfigurationRef().Namespace != nil &&
			*tEnt.GetKonnectAPIAuthConfigurationRef().Namespace != "" {
			namespace = *tEnt.GetKonnectAPIAuthConfigurationRef().Namespace
		}

		ref := tEnt.GetKonnectAPIAuthConfigurationRef()
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: namespace,
				Name:      ref.Name,
			},
		}}
	}
}
