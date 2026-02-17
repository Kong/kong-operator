package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectAPIAuthConfigurationReferencesSecrets is the index field for KonnectAPIAuthConfiguration -> Secret.
	IndexFieldKonnectAPIAuthConfigurationReferencesSecrets = "konnectAPIAuthConfigurationSecretRef" // #nosec G101
)

// OptionsForKonnectAPIAuthConfiguration returns a slice of Option configured for indexing KonnectAPIAuthConfiguration objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForKonnectAPIAuthConfiguration() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectAPIAuthConfiguration{},
			Field:          IndexFieldKonnectAPIAuthConfigurationReferencesSecrets,
			ExtractValueFn: secretsOnKonnectAPIAuthConfiguration,
		},
	}
}

// secretsOnKonnectAPIAuthConfiguration extracts and returns a list of Secret references (in "namespace/name" format).
// from the SecretRef of the given KonnectAPIAuthConfiguration object.
func secretsOnKonnectAPIAuthConfiguration(o client.Object) []string {
	apiAuth, ok := o.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
	if !ok {
		return nil
	}

	// Only return secret reference if the auth config uses SecretRef type.
	if apiAuth.Spec.Type != konnectv1alpha1.KonnectAPIAuthTypeSecretRef || apiAuth.Spec.SecretRef == nil {
		return nil
	}

	secretNamespace := apiAuth.Spec.SecretRef.Namespace
	if secretNamespace == "" {
		secretNamespace = apiAuth.Namespace
	}

	return []string{secretNamespace + "/" + apiAuth.Spec.SecretRef.Name}
}
