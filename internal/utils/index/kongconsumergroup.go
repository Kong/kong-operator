package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

const (
	// IndexFieldKongConsumerGroupOnPlugin is the index field for KongConsumerGroup -> KongPlugin.
	IndexFieldKongConsumerGroupOnPlugin = "consumerGroupPluginRef"
	// IndexFieldKongConsumerGroupOnKonnectGatewayControlPlane is the index field for KongConsumerGroup -> KonnectGatewayControlPlane.
	IndexFieldKongConsumerGroupOnKonnectGatewayControlPlane = "consumerGroupKonnectGatewayControlPlaneRef"
)

// OptionsForKongConsumerGroup returns required Index options for KongConsumerGroup reconciler.
func OptionsForKongConsumerGroup(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1beta1.KongConsumerGroup{},
			Field:          IndexFieldKongConsumerGroupOnPlugin,
			ExtractValueFn: kongConsumerGroupReferencesKongPluginsViaAnnotation,
		},
		{
			Object:         &configurationv1beta1.KongConsumerGroup{},
			Field:          IndexFieldKongConsumerGroupOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1beta1.KongConsumerGroup](cl),
		},
	}
}

func kongConsumerGroupReferencesKongPluginsViaAnnotation(object client.Object) []string {
	consumerGroup, ok := object.(*configurationv1beta1.KongConsumerGroup)
	if !ok {
		return nil
	}
	return metadata.ExtractPluginsWithNamespaces(consumerGroup)
}
