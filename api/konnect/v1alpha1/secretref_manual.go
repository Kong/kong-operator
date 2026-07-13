package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
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
	var config map[string]any
	// Try to unmarshal the secret bytes into a map to check if it's valid JSON. If it is, return it as a JSON object.
	err := json.Unmarshal(secretBytes, &config)
	if err == nil {
		return apiextensionsv1.JSON{Raw: secretBytes}, nil
	}
	// If the secret bytes are not valid JSON, assume that they are in YAML format and convert them to JSON.
	jsonBytes, err := yaml.YAMLToJSON(secretBytes)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to convert YAML to JSON for secret %s/%s: %w",
			namespace, src.SecretRef.Name, err)
	}
	return apiextensionsv1.JSON{Raw: jsonBytes}, nil
}
