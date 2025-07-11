package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

const (
	// IndexFieldKongServiceOnReferencedPluginNames is the index field for KongService -> KongPlugin.
	IndexFieldKongServiceOnReferencedPluginNames = "kongServiceKongPluginRef"
	// IndexFieldKongServiceOnKonnectGatewayControlPlane is the index field for KongService -> KonnectGatewayControlPlane.
	IndexFieldKongServiceOnKonnectGatewayControlPlane = "kongServiceKonnectGatewayControlPlaneRef"
)

// OptionsForKongService returns required Index options for KongService reconciler.
func OptionsForKongService(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnReferencedPluginNames,
			ExtractValueFn: kongServiceUsesPlugins,
		},
		{
			Object:         &configurationv1alpha1.KongService{},
			Field:          IndexFieldKongServiceOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongService](cl),
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
