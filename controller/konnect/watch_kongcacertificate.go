package konnect

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/internal/utils/index"
)

// KongCACertificateReconciliationWatchOptions returns the watch options for the KongCACertificate.
func KongCACertificateReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongCACertificate{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongCACertificate]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1alpha1.KongCACertificateList](
						cl, index.IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongCACertificateList](
						cl, index.IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
					),
				),
			)
		},
	}
}
