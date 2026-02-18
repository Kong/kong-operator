package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongUpstreamOnKonnectGatewayControlPlane is the index field for KongUpstream -> KonnectGatewayControlPlane.
	IndexFieldKongUpstreamOnKonnectGatewayControlPlane = "kongUpstreamKonnectGatewayControlPlaneRef"
)

// OptionsForKongUpstream returns required Index options for KongUpstream reconciler.
func OptionsForKongUpstream(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongUpstream{},
			Field:          IndexFieldKongUpstreamOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongUpstream](cl),
		},
	}
}
