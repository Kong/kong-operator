package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialHMACReferencesKongConsumer is the index name for KongCredentialHMAC -> Consumer.
	IndexFieldKongCredentialHMACReferencesKongConsumer = "kongCredentialsHMACConsumerRef"
	// IndexFieldKongCredentialHMACReferencesSecret is the index name for KongCredentialHMAC -> Secret.
	IndexFieldKongCredentialHMACReferencesSecret = "kongCredentialsHMACSecretRef"
)

// OptionsForCredentialsHMAC returns required Index options for KongCredentialHMAC.
func OptionsForCredentialsHMAC() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCredentialHMAC{},
			Field:          IndexFieldKongCredentialHMACReferencesKongConsumer,
			ExtractValueFn: kongCredentialHMACReferencesConsumer,
		},
		{
			Object:         &configurationv1alpha1.KongCredentialHMAC{},
			Field:          IndexFieldKongCredentialHMACReferencesSecret,
			ExtractValueFn: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialHMAC],
		},
	}
}

// kongCredentialHMACReferencesConsumer returns the name of referenced Consumer.
func kongCredentialHMACReferencesConsumer(obj client.Object) []string {
	cred, ok := obj.(*configurationv1alpha1.KongCredentialHMAC)
	if !ok {
		return nil
	}
	return []string{cred.Spec.ConsumerRef.Name}
}
