package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/annotations"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
)

const (
	// IndexFieldKongConsumerOnKongConsumerGroup is the index field for KongConsumer -> KongConsumerGroup.
	IndexFieldKongConsumerOnKongConsumerGroup = "consumerGroupRef"
	// IndexFieldKongConsumerOnPlugin is the index field for KongConsumer -> KongPlugin.
	IndexFieldKongConsumerOnPlugin = "consumerPluginRef"
)

// IndexOptionsForKongConsumer returns required Index options for KongConsumer reconciler.
func IndexOptionsForKongConsumer() []ReconciliationIndexOption {
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
	return annotations.ExtractPluginsWithNamespaces(consumer)
}
