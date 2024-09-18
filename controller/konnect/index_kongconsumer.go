package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
)

const (
	// IndexFieldKongConsumerReferencesSecrets is the index field for Consumer -> Secret.
	IndexFieldKongConsumerReferencesSecrets = "kongConsumerSecretRef"
)

// IndexOptionsForKongConsumer returns required Index options for Kong Consumer.
func IndexOptionsForKongConsumer() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1.KongConsumer{},
			IndexField:   IndexFieldKongConsumerReferencesSecrets,
			ExtractValue: kongConsumerReferencesSecret,
		},
	}
}

// kongConsumerReferencesSecret returns name of referenced Secrets.
func kongConsumerReferencesSecret(obj client.Object) []string {
	consumer, ok := obj.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}
	return consumer.Credentials
}
