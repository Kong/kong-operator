package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialAPIKeyReferencesKongConsumer is the index name for KongCredentialAPIKey -> Consumer.
	IndexFieldKongCredentialAPIKeyReferencesKongConsumer = "kongCredentialsAPIKeyConsumerRef"
	// IndexFieldKongCredentialAPIKeyReferencesSecret is the index name for KongCredentialAPIKey -> Secret.
	IndexFieldKongCredentialAPIKeyReferencesSecret = "kongCredentialsAPIKeySecretRef"
)

// OptionsForCredentialsAPIKey returns required Index options for KongCredentialAPIKey.
func OptionsForCredentialsAPIKey() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCredentialAPIKey{},
			Field:          IndexFieldKongCredentialAPIKeyReferencesKongConsumer,
			ExtractValueFn: kongCredentialAPIKeyReferencesConsumer,
		},
		{
			Object:         &configurationv1alpha1.KongCredentialAPIKey{},
			Field:          IndexFieldKongCredentialAPIKeyReferencesSecret,
			ExtractValueFn: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialAPIKey],
		},
	}
}

// kongCredentialAPIKeyReferencesConsumer returns the name of referenced Consumer.
func kongCredentialAPIKeyReferencesConsumer(obj client.Object) []string {
	cred, ok := obj.(*configurationv1alpha1.KongCredentialAPIKey)
	if !ok {
		return nil
	}
	return []string{cred.Spec.ConsumerRef.Name}
}
