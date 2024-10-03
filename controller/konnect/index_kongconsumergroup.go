package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/annotations"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

const (
	// IndexFieldKongConsumerGroupOnPlugin is the index field for KongConsumerGroup -> KongPlugin.
	IndexFieldKongConsumerGroupOnPlugin = "consumerGroupPluginRef"
)

// IndexOptionsForKongConsumerGroup returns required Index options for KongConsumerGroup reconciler.
func IndexOptionsForKongConsumerGroup() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1beta1.KongConsumerGroup{},
			IndexField:   IndexFieldKongConsumerGroupOnPlugin,
			ExtractValue: kongConsumerGroupReferencesKongPluginsViaAnnotation,
		},
	}
}

func kongConsumerGroupReferencesKongPluginsViaAnnotation(object client.Object) []string {
	consumerGroup, ok := object.(*configurationv1beta1.KongConsumerGroup)
	if !ok {
		return nil
	}
	return annotations.ExtractPluginsWithNamespaces(consumerGroup)
}
