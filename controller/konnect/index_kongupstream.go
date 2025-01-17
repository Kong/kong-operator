package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongUpstreamOnKonnectGatewayControlPlane is the index field for KongUpstream -> KonnectGatewayControlPlane.
	IndexFieldKongUpstreamOnKonnectGatewayControlPlane = "kongUpstreamKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongUpstream returns required Index options for KongUpstream reconciler.
func IndexOptionsForKongUpstream(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongUpstream{},
			IndexField:   IndexFieldKongUpstreamOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongUpstream](cl),
		},
	}
}
