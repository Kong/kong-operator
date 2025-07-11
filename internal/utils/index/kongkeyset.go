package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongKeySetOnKonnectGatewayControlPlane is the index field for KongKeySet -> KonnectGatewayControlPlane.
	IndexFieldKongKeySetOnKonnectGatewayControlPlane = "kongKeySetKonnectGatewayControlPlaneRef"
)

// OptionsForKongKeySet returns required Index options for KongKeySet reconciler.
func OptionsForKongKeySet(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongKeySet{},
			Field:          IndexFieldKongKeySetOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongKeySet](cl),
		},
	}
}
