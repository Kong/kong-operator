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
func IndexOptionsForKongKeySet() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongKeySet{},
			IndexField:   IndexFieldKongKeySetOnKonnectGatewayControlPlane,
			ExtractValue: konnectGatewayControlPlaneRefFromKongKeySet,
		},
	}
}

// konnectGatewayControlPlaneRefFromKongKeySet returns namespace/name of referenced KonnectGatewayControlPlane in KongKeySet spec.
func konnectGatewayControlPlaneRefFromKongKeySet(obj client.Object) []string {
	keySet, ok := obj.(*configurationv1alpha1.KongKeySet)
	if !ok {
		return nil
	}
	return controlPlaneKonnectNamespacedRefAsSlice(keySet)
}
