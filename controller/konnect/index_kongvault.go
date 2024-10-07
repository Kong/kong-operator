package konnect

import (
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// IndexFieldKongVaultOnKonnectGatewayControlPlane is the index field for KongVault -> KonnectGatewayControlPlane.
	IndexFieldKongVaultOnKonnectGatewayControlPlane = "vaultKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongVault returns required Index options for KongVault reconciler.
func IndexOptionsForKongVault() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongVault{},
			IndexField:   IndexFieldKongVaultOnKonnectGatewayControlPlane,
			ExtractValue: kongVaultReferencesKonnectGatewayControlPlane,
		},
	}
}

func kongVaultReferencesKonnectGatewayControlPlane(object client.Object) []string {
	vault, ok := object.(*configurationv1alpha1.KongVault)
	if !ok {
		return nil
	}

	return controlPlaneKonnectNamespacedRefAsSlice(vault)
}
