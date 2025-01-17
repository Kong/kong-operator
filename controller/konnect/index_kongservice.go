package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

const (
	// IndexFieldKongServiceOnReferencedPluginNames is the index field for KongService -> KongPlugin.
	IndexFieldKongServiceOnReferencedPluginNames = "kongServiceKongPluginRef"
	// IndexFieldKongServiceOnKonnectGatewayControlPlane is the index field for KongService -> KonnectGatewayControlPlane.
	IndexFieldKongServiceOnKonnectGatewayControlPlane = "kongServiceKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongService returns required Index options for KongService reconciler.
func IndexOptionsForKongService(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongService{},
			IndexField:   IndexFieldKongServiceOnReferencedPluginNames,
			ExtractValue: kongServiceUsesPlugins,
		},
		{
			IndexObject:  &configurationv1alpha1.KongService{},
			IndexField:   IndexFieldKongServiceOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongService](cl),
		},
	}
}

func kongServiceUsesPlugins(object client.Object) []string {
	svc, ok := object.(*configurationv1alpha1.KongService)
	if !ok {
		return nil
	}

	return metadata.ExtractPluginsWithNamespaces(svc)
}
