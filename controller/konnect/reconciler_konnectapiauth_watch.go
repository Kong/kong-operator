package konnect

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// listKonnectAPIAuthConfigurationsReferencingSecret returns a function that lists
// KonnectAPIAuthConfiguration resources that reference the given Secret.
// This function is intended to be used as a handler for the watch on Secrets.
// NOTE: The Secret has to have the konnect.konghq.com/credential=konnect set
// so that we can efficiently watch only the relevant Secrets' changes.
func listKonnectAPIAuthConfigurationsReferencingSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		logger := log.FromContext(ctx)

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
			log.FromContext(ctx).Error(
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
