package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongKeySetOnKonnectGatewayControlPlane is the index field for KongKeySet -> KonnectGatewayControlPlane.
	IndexFieldKongKeySetOnKonnectGatewayControlPlane = "kongKeySetKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongKeySet returns required Index options for KongKeySet reconclier.
func IndexOptionsForKongKeySet(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongKeySet{},
			IndexField:   IndexFieldKongKeySetOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongKeySet](cl),
		},
	}
}
