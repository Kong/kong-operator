package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialACLReferencesKongConsumer is the index name for KongCredentialACL -> Consumer.
	IndexFieldKongCredentialACLReferencesKongConsumer = "kongCredentialsACLConsumerRef"
	// IndexFieldKongCredentialACLReferencesKongSecret is the index name for KongCredentialACL -> Secret.
	IndexFieldKongCredentialACLReferencesKongSecret = "kongCredentialsACLSecretRef"
)

// OptionsForCredentialsACL returns required Index options for KongCredentialACL.
func OptionsForCredentialsACL() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCredentialACL{},
			Field:          IndexFieldKongCredentialACLReferencesKongConsumer,
			ExtractValueFn: kongCredentialACLReferencesConsumer,
		},
		{
			Object:         &configurationv1alpha1.KongCredentialACL{},
			Field:          IndexFieldKongCredentialACLReferencesKongSecret,
			ExtractValueFn: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialACL],
		},
	}
}

// kongCredentialACLReferencesConsumer returns the name of referenced Consumer.
func kongCredentialACLReferencesConsumer(obj client.Object) []string {
	cred, ok := obj.(*configurationv1alpha1.KongCredentialACL)
	if !ok {
		return nil
	}
	return []string{cred.Spec.ConsumerRef.Name}
}
