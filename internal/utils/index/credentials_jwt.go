package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialJWTReferencesKongConsumer is the index name for KongCredentialJWT -> Consumer.
	IndexFieldKongCredentialJWTReferencesKongConsumer = "kongCredentialsJWTConsumerRef"
	// IndexFieldKongCredentialJWTReferencesSecret is the index name for KongCredentialJWT -> Secret.
	IndexFieldKongCredentialJWTReferencesSecret = "kongCredentialsJWTSecretRef"
)

// OptionsForCredentialsJWT returns required Index options for KongCredentialJWT.
func OptionsForCredentialsJWT() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCredentialJWT{},
			Field:          IndexFieldKongCredentialJWTReferencesKongConsumer,
			ExtractValueFn: kongCredentialJWTReferencesConsumer,
		},
		{
			Object:         &configurationv1alpha1.KongCredentialJWT{},
			Field:          IndexFieldKongCredentialJWTReferencesSecret,
			ExtractValueFn: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialJWT],
		},
	}
}

// kongCredentialJWTReferencesConsumer returns the name of referenced Consumer.
func kongCredentialJWTReferencesConsumer(obj client.Object) []string {
	cred, ok := obj.(*configurationv1alpha1.KongCredentialJWT)
	if !ok {
		return nil
	}
	return []string{cred.Spec.ConsumerRef.Name}
}
