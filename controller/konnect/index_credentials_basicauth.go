package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldCredentialBasicAuthReferencesKongConsumer is the index name for CredentialBasicAuth -> Consumer.
	IndexFieldCredentialBasicAuthReferencesKongConsumer = "kongCredentialsBasicAuthConsumerRef"
)

// IndexOptionsForCredentialsBasicAuth returns required Index options for CredentialBasicAuth.
func IndexOptionsForCredentialsBasicAuth() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.CredentialBasicAuth{},
			IndexField:   IndexFieldCredentialBasicAuthReferencesKongConsumer,
			ExtractValue: kongCredentialBasicAuthReferencesConsumer,
		},
	}
}

// kongCredentialBasicAuthReferencesConsumer returns the name of referenced Consumer.
func kongCredentialBasicAuthReferencesConsumer(obj client.Object) []string {
	cred, ok := obj.(*configurationv1alpha1.CredentialBasicAuth)
	if !ok {
		return nil
	}
	return []string{cred.Spec.ConsumerRef.Name}
}
