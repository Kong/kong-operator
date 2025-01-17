package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongKeyOnKongKeySetReference is the index field for KongKey-> KongKeySet.
	IndexFieldKongKeyOnKongKeySetReference = "kongKeySetRef"

	// IndexFieldKongKeyOnKonnectGatewayControlPlane is the index field for KongKey -> KonnectGatewayControlPlane.
	IndexFieldKongKeyOnKonnectGatewayControlPlane = "kongKeyKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongKey returns required Index options for KongKey reconclier.
func IndexOptionsForKongKey(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongKey{},
			IndexField:   IndexFieldKongKeyOnKongKeySetReference,
			ExtractValue: kongKeySetRefFromKongKey,
		},
		{
			IndexObject:  &configurationv1alpha1.KongKey{},
			IndexField:   IndexFieldKongKeyOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongKey](cl),
		},
	}
}

// kongKeySetRefFromKongKey returns namespace/name of referenced KongKeySet in KongKey spec.
func kongKeySetRefFromKongKey(obj client.Object) []string {
	key, ok := obj.(*configurationv1alpha1.KongKey)
	if !ok {
		return nil
	}

	if key.Spec.KeySetRef == nil ||
		key.Spec.KeySetRef.Type != configurationv1alpha1.KeySetRefNamespacedRef ||
		key.Spec.KeySetRef.NamespacedRef == nil {
		return nil
	}

	return []string{key.GetNamespace() + "/" + key.Spec.KeySetRef.NamespacedRef.Name}
}
