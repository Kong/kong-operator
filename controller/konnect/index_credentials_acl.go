package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCredentialACLReferencesKongConsumer is the index name for KongCredentialACL -> Consumer.
	IndexFieldKongCredentialACLReferencesKongConsumer = "kongCredentialsACLConsumerRef"
	// IndexFieldKongCredentialACLReferencesKongSecret is the index name for KongCredentialACL -> Secret.
	IndexFieldKongCredentialACLReferencesKongSecret = "kongCredentialsACLSecretRef"
)

// IndexOptionsForCredentialsACL returns required Index options for KongCredentialACL.
func IndexOptionsForCredentialsACL() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongCredentialACL{},
			IndexField:   IndexFieldKongCredentialACLReferencesKongConsumer,
			ExtractValue: kongCredentialACLReferencesConsumer,
		},
		{
			IndexObject:  &configurationv1alpha1.KongCredentialACL{},
			IndexField:   IndexFieldKongCredentialACLReferencesKongSecret,
			ExtractValue: kongCredentialReferencesSecret[configurationv1alpha1.KongCredentialACL],
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
