package konnect

import (
	"context"

	corev1 "k8s.io/api/core/v1"
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

// KongCertificateReconciliationWatchOptions returns the watch options for the KongCertificate.
func KongCertificateReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongCertificate{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongCertificate]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1alpha1.KongCertificateList](
						cl, index.IndexFieldKongCertificateOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongCertificateList](
						cl, index.IndexFieldKongCertificateOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&corev1.Secret{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongCertificateForSecret(cl),
				),
			)
		},
	}
}

// enqueueKongCertificateForSecret enqueues KongCertificate reconcile requests
// for KongCertificates that reference the given Secret.
func enqueueKongCertificateForSecret(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		// List all KongCertificates that reference this Secret.
		var certList configurationv1alpha1.KongCertificateList
		if err := cl.List(ctx, &certList, client.MatchingFields{
			index.IndexFieldKongCertificateReferencesSecrets: secret.Namespace + "/" + secret.Name,
		}); err != nil {
			return nil
		}

		requests := make([]reconcile.Request, 0, len(certList.Items))
		for _, cert := range certList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name:      cert.Name,
					Namespace: cert.Namespace,
				},
			})
		}
		return requests
	}
}
