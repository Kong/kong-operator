package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/pkg/metadata"
)

const (
	// IndexFieldKongConsumerOnKongConsumerGroup is the index field for KongConsumer -> KongConsumerGroup.
	IndexFieldKongConsumerOnKongConsumerGroup = "consumerGroupRef"
	// IndexFieldKongConsumerOnPlugin is the index field for KongConsumer -> KongPlugin.
	IndexFieldKongConsumerOnPlugin = "consumerPluginRef"
	// IndexFieldKongConsumerOnKonnectGatewayControlPlane is the index field for KongConsumer -> KonnectGatewayControlPlane.
	IndexFieldKongConsumerOnKonnectGatewayControlPlane = "consumerKonnectGatewayControlPlaneRef"
	// IndexFieldKongConsumerReferencesSecrets is the index field for Consumer -> Secret.
	IndexFieldKongConsumerReferencesSecrets = "kongConsumerSecretRef"
)

// OptionsForKongConsumer returns required Index options for KongConsumer reconciler.
func OptionsForKongConsumer(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1.KongConsumer{},
			Field:          IndexFieldKongConsumerOnKongConsumerGroup,
			ExtractValueFn: kongConsumerReferencesFromKongConsumerGroup,
		},
		{
			Object:         &configurationv1.KongConsumer{},
			Field:          IndexFieldKongConsumerOnPlugin,
			ExtractValueFn: kongConsumerReferencesKongPluginsViaAnnotation,
		},
		{
			Object:         &configurationv1.KongConsumer{},
			Field:          IndexFieldKongConsumerOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1.KongConsumer](cl),
		},
		{
			Object:         &configurationv1.KongConsumer{},
			Field:          IndexFieldKongConsumerReferencesSecrets,
			ExtractValueFn: kongConsumerReferencesSecrets,
		},
	}
}

func kongConsumerReferencesFromKongConsumerGroup(object client.Object) []string {
	consumer, ok := object.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}
	return consumer.ConsumerGroups
}

func kongConsumerReferencesKongPluginsViaAnnotation(object client.Object) []string {
	consumer, ok := object.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}
	return metadata.ExtractPluginsWithNamespaces(consumer)
}

// kongConsumerReferencesSecret returns name of referenced Secrets.
func kongConsumerReferencesSecrets(obj client.Object) []string {
	consumer, ok := obj.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}
	return consumer.Credentials
}
