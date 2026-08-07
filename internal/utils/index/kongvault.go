package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongVaultOnKonnectGatewayControlPlane is the index field for KongVault -> KonnectGatewayControlPlane.
	IndexFieldKongVaultOnKonnectGatewayControlPlane = "vaultKonnectGatewayControlPlaneRef"
	// IndexFieldKongVaultOnKonnectConfigStore is the index field for KongVault -> KonnectConfigStore.
	IndexFieldKongVaultOnKonnectConfigStore = "vaultKonnectConfigStoreRef"
)

// OptionsForKongVault returns required Index options for KongVault reconciler.
func OptionsForKongVault(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongVault{},
			Field:          IndexFieldKongVaultOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongVault](cl),
		},
		{
			Object:         &configurationv1alpha1.KongVault{},
			Field:          IndexFieldKongVaultOnKonnectConfigStore,
			ExtractValueFn: kongVaultOnKonnectConfigStoreRef,
		},
	}
}

func kongVaultOnKonnectConfigStoreRef(object client.Object) []string {
	vault, ok := object.(*configurationv1alpha1.KongVault)
	if !ok {
		return nil
	}
	// KongVault is cluster-scoped, so the reference carries its own namespace and
	// there is nothing to default it to.
	nn, ok := vault.GetConfigStoreRefNamespacedName()
	if !ok {
		return nil
	}
	return []string{nn.String()}
}
