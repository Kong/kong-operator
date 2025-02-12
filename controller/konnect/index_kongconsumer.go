package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
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

// IndexOptionsForKongConsumer returns required Index options for KongConsumer reconciler.
func IndexOptionsForKongConsumer(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1.KongConsumer{},
			IndexField:   IndexFieldKongConsumerOnKongConsumerGroup,
			ExtractValue: kongConsumerReferencesFromKongConsumerGroup,
		},
		{
			IndexObject:  &configurationv1.KongConsumer{},
			IndexField:   IndexFieldKongConsumerOnPlugin,
			ExtractValue: kongConsumerReferencesKongPluginsViaAnnotation,
		},
		{
			IndexObject:  &configurationv1.KongConsumer{},
			IndexField:   IndexFieldKongConsumerOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1.KongConsumer](cl),
		},
		{
			IndexObject:  &configurationv1.KongConsumer{},
			IndexField:   IndexFieldKongConsumerReferencesSecrets,
			ExtractValue: kongConsumerReferencesSecrets,
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
