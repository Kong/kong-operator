package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialAPIKeyReferencesKongConsumer is the index name for KongCredentialAPIKey -> Consumer.
	IndexFieldKongCredentialAPIKeyReferencesKongConsumer = "kongCredentialsAPIKeyConsumerRef"
	// IndexFieldKongCredentialAPIKeyReferencesSecret is the index name for KongCredentialAPIKey -> Secret.
	IndexFieldKongCredentialAPIKeyReferencesSecret = "kongCredentialsAPIKeySecretRef"
)

// IndexOptionsForCredentialsAPIKey returns required Index options for KongCredentialAPIKey.
func IndexOptionsForCredentialsAPIKey() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongCredentialAPIKey{},
			IndexField:   IndexFieldKongCredentialAPIKeyReferencesKongConsumer,
			ExtractValue: kongCredentialAPIKeyReferencesConsumer,
		},
		{
			IndexObject:  &configurationv1alpha1.KongCredentialAPIKey{},
			IndexField:   IndexFieldKongCredentialAPIKeyReferencesSecret,
			ExtractValue: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialAPIKey],
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
