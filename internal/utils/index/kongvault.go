package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongVaultOnKonnectGatewayControlPlane is the index field for KongVault -> KonnectGatewayControlPlane.
	IndexFieldKongVaultOnKonnectGatewayControlPlane = "vaultKonnectGatewayControlPlaneRef"
)

// OptionsForKongVault returns required Index options for KongVault reconciler.
func OptionsForKongVault(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongVault{},
			Field:          IndexFieldKongVaultOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongVault](cl),
		},
	}
}
