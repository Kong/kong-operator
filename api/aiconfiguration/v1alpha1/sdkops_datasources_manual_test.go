package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestSDKOpsAPISpec_SecretInSliceDoesNotMutateObject guards the deep copy in
// the generated sdkOpsAPISpec: a Secret-sourced leaf inside a slice under a
// union variant shares its backing array with the object, so resolving into
// a shallow copy would leak the Secret value into the object (and the cache).
func TestSDKOpsAPISpec_SecretInSliceDoesNotMutateObject(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		Name: "openai-creds", Namespace: "default",
		Data: map[string][]byte{"authorization": []byte("Bearer sk-secret")},
	}).Build()

	obj := &AIGatewayModelProvider{
		Name: "provider", Namespace: "default",
		Spec: AIGatewayModelProviderSpec{
			APISpec: AIGatewayModelProviderAPISpec{
				AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
					Type: AIGatewayModelProviderConfigTypeOpenai,
					Openai: &AIGatewayModelProviderOpenai{
						Name:        "openai-provider",
						DisplayName: "OpenAI",
						Config: AIGatewayModelProviderOpenaiConfig{
							Auth: AIGatewayModelProviderConfigAuthBasic{
								Headers: []AIGatewayModelProviderConfigAuthBasicHeaders{{
									Name: "Authorization",
									Value: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "openai-creds", Key: "authorization"},
									},
								}},
							},
						},
					},
				},
			},
		},
	}

	resolved, err := obj.sdkOpsAPISpec(t.Context(), cl)
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk-secret", resolved.Openai.Config.Auth.Headers[0].Value.GetValue())
	assert.Nil(t, obj.Spec.APISpec.Openai.Config.Auth.Headers[0].Value.Value, "resolved Secret value leaked into the object")
}
