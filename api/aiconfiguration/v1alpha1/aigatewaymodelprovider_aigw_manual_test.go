package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayModelProvider_ToAIGWProvider covers the basic-auth and azure variants, the
// managed_by drop, and secretRef resolution (whose values must appear in the output without
// being written back into the caller's spec).
func TestAIGatewayModelProvider_ToAIGWProvider(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	secret := &corev1.Secret{
		Name: "azure-creds", Namespace: "default",
		Data: map[string][]byte{"client-secret": []byte("hunter2")},
	}

	tests := []struct {
		name    string
		obj     *AIGatewayModelProvider
		want    *aigw.Provider
		wantErr string
	}{
		{
			name: "openai basic auth, labels kept, managed_by dropped",
			obj: &AIGatewayModelProvider{
				Name: "sample-ai-gw-provider-openai", Namespace: "default",
				Spec: AIGatewayModelProviderSpec{
					APISpec: AIGatewayModelProviderAPISpec{
						AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
							Type: AIGatewayModelProviderConfigTypeOpenai,
							Openai: &AIGatewayModelProviderOpenai{
								Name:        "openai-provider",
								DisplayName: "OpenAI",
								Labels:      PublicLabels{"app": "test1", "env": "test"},
								ManagedBy:   ManagedBy{"kong-operator": "true"},
								Config: AIGatewayModelProviderOpenaiConfig{
									Auth: AIGatewayModelProviderConfigAuthBasic{
										Headers: []AIGatewayModelProviderConfigAuthBasicHeaders{
											{
												Name: "Authorization",
												Value: SensitiveDataSource{
													Type:  SensitiveDataSourceTypeInline,
													Value: new("Bearer sk-123"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: &aigw.Provider{
				Type:        "openai",
				Name:        "openai-provider",
				DisplayName: "OpenAI",
				Labels:      aigw.Labels{"app": "test1", "env": "test"},
				Config: aigw.ProviderConfig{
					Auth: aigw.ProviderAuth{
						Type:    "basic",
						Headers: []aigw.AuthHeader{{Name: "Authorization", Value: "Bearer sk-123"}},
					},
				},
			},
		},
		{
			name: "azure, secretRef resolved, auth union flattened",
			obj: &AIGatewayModelProvider{
				Name: "sample-ai-gw-provider-azure", Namespace: "default",
				Spec: AIGatewayModelProviderSpec{
					APISpec: AIGatewayModelProviderAPISpec{
						AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
							Type: AIGatewayModelProviderConfigTypeAzure,
							Azure: &AIGatewayModelProviderAzure{
								Name:        "azure-provider",
								DisplayName: "Azure",
								Config: AIGatewayModelProviderAzureConfig{
									Service:  "azure-openai",
									Instance: "my-instance",
									Auth: &AIGatewayModelProviderAzureConfigAuth{
										Type: AIGatewayModelProviderAzureConfigAuthTypeAzure,
										Azure: &AIGatewayModelProviderConfigAuthAzure{
											ClientID: "my-client-id",
											ClientSecret: SensitiveDataSource{
												Type: SensitiveDataSourceTypeSecretRef,
												SecretRef: &SensitiveDataSecretRef{
													Name: "azure-creds",
													Key:  "client-secret",
												},
											},
											TenantID: "my-tenant-id",
										},
									},
								},
							},
						},
					},
				},
			},
			want: &aigw.Provider{
				Type:        "azure",
				Name:        "azure-provider",
				DisplayName: "Azure",
				Config: aigw.ProviderConfig{
					Service:  "azure-openai",
					Instance: "my-instance",
					Auth: aigw.ProviderAuth{
						Type:         "azure",
						ClientID:     "my-client-id",
						ClientSecret: "hunter2",
						TenantID:     "my-tenant-id",
					},
				},
			},
		},
		{
			name: "nil config",
			obj: &AIGatewayModelProvider{
				Name: "no-config", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
			got, err := tt.obj.ToAIGWProvider(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	// sdkOpsAPISpec writes resolved values back into the spec it walks; ToAIGWProvider must do
	// that on a copy. Pin that the caller's object still holds the secretRef after conversion.
	obj := tests[1].obj
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	_, err := obj.ToAIGWProvider(t.Context(), cl)
	require.NoError(t, err)
	require.Nil(t, obj.Spec.APISpec.Azure.Config.Auth.Azure.ClientSecret.Value,
		"secretRef must not be resolved in place on the caller's object")
}

// TestAIGatewayModelProvider_ToAIGWProvider_StrictRoundTrip guards against a dropped or renamed
// field: it decodes the payload with yaml.v3's KnownFields(true), which errors on any key
// aigw.Provider (and its non-custom-unmarshaled sub-types) doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayModelProvider_ToAIGWProvider_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayModelProviderAPISpec{
		AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
			Type: AIGatewayModelProviderConfigTypeOpenai,
			Openai: &AIGatewayModelProviderOpenai{
				Name:        "openai-provider",
				DisplayName: "OpenAI",
				Labels:      PublicLabels{"app": "test1"},
			},
		},
	}
	data, err := func() ([]byte, error) {
		payload, err := spec.marshalSDKOpsPayload()
		if err != nil {
			return nil, err
		}
		cfg, ok := payload[string(spec.Type)].(map[string]any)
		if !ok {
			return nil, err
		}
		delete(cfg, "managed_by")
		data, _, err := spec.selectedSDKOpsPayload(payload)
		return data, err
	}()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var provider aigw.Provider
	require.NoError(t, dec.Decode(&provider))

	// Pin the discriminator, which selectedSDKOpsPayload copies in from the top-level payload.
	require.Equal(t, "openai", provider.Type)
	require.Equal(t, "openai-provider", provider.Name)
}
