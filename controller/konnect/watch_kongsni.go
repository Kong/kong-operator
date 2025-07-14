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

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/internal/utils/index"
)

// KongSNIReconciliationWatchOptions returns the watch options for
// the KongSNI.
func KongSNIReconciliationWatchOptions(cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(
				&configurationv1alpha1.KongSNI{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(kongSNIRefersToKonnectGatewayControlPlane(cl)),
				),
			)
		},

		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongCertificate{},
				handler.EnqueueRequestsFromMapFunc(enqueueKongSNIForKongCertificate(cl)),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongCertificate]),
				),
			)
		},
	}
}

func kongSNIRefersToKonnectGatewayControlPlane(
	cl client.Client,
) func(client.Object) bool {
	return func(obj client.Object) bool {
		sni, ok := obj.(*configurationv1alpha1.KongSNI)
		if !ok {
			return false
		}

		certNN := types.NamespacedName{
			Namespace: sni.Namespace,
			Name:      sni.Spec.CertificateRef.Name,
		}
		cert := configurationv1alpha1.KongCertificate{}
		if err := cl.Get(context.Background(), certNN, &cert); err != nil {
			return true
		}

		return objHasControlPlaneRef(&cert)
	}
}

func enqueueKongSNIForKongCertificate(
	cl client.Client,
) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cert, ok := obj.(*configurationv1alpha1.KongCertificate)
		if !ok {
			return nil
		}

		if !objHasControlPlaneRef(cert) {
			return nil
		}

		sniList := configurationv1alpha1.KongSNIList{}
		if err := cl.List(ctx, &sniList, client.InNamespace(cert.Namespace),
			client.MatchingFields{
				index.IndexFieldKongSNIOnCertificateRefName: cert.Name,
			},
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(sniList.Items)
	}
}
