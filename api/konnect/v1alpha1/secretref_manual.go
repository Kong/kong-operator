package v1alpha1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// valueFromSecretRef converts a SecretRef to a JSON value by fetching the referenced Secret and extracting the specified key.
func (src *AIGatewayPolicyConfigDataSource) valueFromSecretRef(ctx context.Context, cl client.Client, namespace string) (apiextensionsv1.JSON, error) {
	if src.SecretRef == nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("secretRef is nil for AIGatewayPolicyConfigDataSource")
	}
	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: src.SecretRef.Name}, &secret); err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to fetch Secret %s/%s: %w", namespace, src.SecretRef.Name, err)
	}
	secretBytes, ok := secret.Data[src.SecretRef.Key]
	if !ok {
		return apiextensionsv1.JSON{}, fmt.Errorf("secret %s/%s is missing key %q", namespace, src.SecretRef.Name, src.SecretRef.Key)
	}
	// REVIEW: we were validating if the secret contains a valid JSON or YAML in KongPlugin.
	// Should we also implement it here?
	return apiextensionsv1.JSON{Raw: secretBytes}, nil
}
