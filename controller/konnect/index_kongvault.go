package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongVaultOnKonnectGatewayControlPlane is the index field for KongVault -> KonnectGatewayControlPlane.
	IndexFieldKongVaultOnKonnectGatewayControlPlane = "vaultKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongVault returns required Index options for KongVault reconciler.
func IndexOptionsForKongVault(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongVault{},
			IndexField:   IndexFieldKongVaultOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongVault](cl),
		},
	}
}
