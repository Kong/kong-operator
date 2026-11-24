package v1alpha1

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestAIGatewayModelProvider_ToAIGWProvider covers the basic-auth and azure variants, the
// managed_by drop, and secretRef resolution (whose values must appear in the output without
// being written back into the caller's spec).
func TestAIGatewayModelProvider_ToAIGWProvider(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	// newAzureSecret returns a fresh object per subtest: the fake client's tracker mutates the
	// objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newAzureSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "azure-creds", Namespace: "default",
			Data: map[string][]byte{"client-secret": []byte("hunter2")},
		}
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
			name: "cross-namespace secretRef without KongReferenceGrant rejected",
			obj: &AIGatewayModelProvider{
				Name: "sample-ai-gw-provider-cross-ns", Namespace: "default",
				Spec: AIGatewayModelProviderSpec{
					APISpec: AIGatewayModelProviderAPISpec{
						AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
							Type: AIGatewayModelProviderConfigTypeAzure,
							Azure: &AIGatewayModelProviderAzure{
								Name:        "azure-provider",
								DisplayName: "Azure",
								Config: AIGatewayModelProviderAzureConfig{
									Auth: &AIGatewayModelProviderAzureConfigAuth{
										Type: AIGatewayModelProviderAzureConfigAuthTypeAzure,
										Azure: &AIGatewayModelProviderConfigAuthAzure{
											ClientSecret: SensitiveDataSource{
												Type: SensitiveDataSourceTypeSecretRef,
												SecretRef: &SensitiveDataSecretRef{
													Name:      "azure-creds",
													Key:       "client-secret",
													Namespace: new("other-namespace"),
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
			wantErr: "is not granted by any KongReferenceGrant",
		},
		{
			name: "cross-namespace header secretRef without KongReferenceGrant rejected",
			obj: &AIGatewayModelProvider{
				Name: "sample-ai-gw-provider-cross-ns-header", Namespace: "default",
				Spec: AIGatewayModelProviderSpec{
					APISpec: AIGatewayModelProviderAPISpec{
						AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
							Type: AIGatewayModelProviderConfigTypeOpenai,
							Openai: &AIGatewayModelProviderOpenai{
								Name:        "openai-provider",
								DisplayName: "OpenAI",
								Config: AIGatewayModelProviderOpenaiConfig{
									Auth: AIGatewayModelProviderConfigAuthBasic{
										Headers: []AIGatewayModelProviderConfigAuthBasicHeaders{
											{
												Name: "Authorization",
												Value: SensitiveDataSource{
													Type: SensitiveDataSourceTypeSecretRef,
													SecretRef: &SensitiveDataSecretRef{
														Name:      "openai-creds",
														Key:       "api-key",
														Namespace: new("other-namespace"),
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
			},
			wantErr: "is not granted by any KongReferenceGrant",
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

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newAzureSecret()).Build()
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
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newAzureSecret()).Build()
	_, err := obj.ToAIGWProvider(t.Context(), cl)
	require.NoError(t, err)
	require.Nil(t, obj.Spec.APISpec.Azure.Config.Auth.Azure.ClientSecret.Value,
		"secretRef must not be resolved in place on the caller's object")
}

// TestAIGatewayModelProvider_GeminiWorkloadIdentityFederation covers the gemini GCP workload
// identity federation aws_iam arm on the on-prem and the Konnect paths.
func TestAIGatewayModelProvider_GeminiWorkloadIdentityFederation(t *testing.T) {
	t.Parallel()

	newObj := func() *AIGatewayModelProvider {
		return &AIGatewayModelProvider{
			Name: "gemini-federated", Namespace: "default",
			Spec: AIGatewayModelProviderSpec{
				APISpec: AIGatewayModelProviderAPISpec{
					AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
						Type: AIGatewayModelProviderConfigTypeGemini,
						Gemini: &AIGatewayModelProviderGemini{
							Name:        "gemini-federated",
							DisplayName: "Gemini federated",
							Config: AIGatewayModelProviderGeminiConfig{
								Auth: &AIGatewayModelProviderGeminiConfigAuth{
									Type: AIGatewayModelProviderGeminiConfigAuthTypeGCP,
									GCP: &AIGatewayModelProviderConfigAuthGCP{
										WorkloadIdentityFederation: &AIGatewayModelProviderConfigAuthGCPWorkloadIdentityFederation{
											Source: AIGatewayModelProviderConfigAuthGCPWorkloadIdentityFederationTypeAuthGCPWorkloadIdentityFederationAwsIam,
											AuthGCPWorkloadIdentityFederationAwsIam: &AuthGCPWorkloadIdentityFederationAwsIam{
												AuthJSON: "{vault://ai/gcp-auth-json}",
												Aws: AIGatewayUpstreamAuthAWS{
													AccessKeyID:     "{vault://env/aws-key-id}",
													SecretAccessKey: "{vault://env/aws-secret}",
													SessionToken:    "{vault://env/aws-session-token}",
													Region:          "us-east-1",
													AssumeRoleArn:   "arn:aws:iam::123456789012:role/bedrock",
													RoleSessionName: "kong-session",
													StsEndpointURL:  "https://sts.us-east-1.amazonaws.com",
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
		}
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	t.Run("on-prem", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		got, err := newObj().ToAIGWProvider(t.Context(), cl)
		require.NoError(t, err)

		require.Equal(t, "gcp", got.Config.Auth.Type)
		wif := got.Config.Auth.WorkloadIdentityFederation
		require.NotNil(t, wif)
		require.Equal(t, "aws_iam", wif.Source)
		require.Equal(t, "{vault://ai/gcp-auth-json}", wif.AuthJSON)
		require.Equal(t, "{vault://env/aws-key-id}", wif.AWSAccessKeyID)
		require.Equal(t, "{vault://env/aws-secret}", wif.AWSSecretAccessKey)
		require.Equal(t, "{vault://env/aws-session-token}", wif.AWSSessionToken)
		require.Equal(t, "us-east-1", wif.AWSRegion)
		require.Equal(t, "arn:aws:iam::123456789012:role/bedrock", wif.AWSAssumeRoleARN)
		require.Equal(t, "kong-session", wif.AWSRoleSessionName)
		require.Equal(t, "https://sts.us-east-1.amazonaws.com", wif.AWSSTSEndpointURL)
	})

	wantKonnectWIF := `{
		"source": "aws_iam",
		"auth_json": "{vault://ai/gcp-auth-json}",
		"aws": {
			"type": "aws",
			"access_key_id": "{vault://env/aws-key-id}",
			"secret_access_key": "{vault://env/aws-secret}",
			"session_token": "{vault://env/aws-session-token}",
			"region": "us-east-1",
			"assume_role_arn": "arn:aws:iam::123456789012:role/bedrock",
			"role_session_name": "kong-session",
			"sts_endpoint_url": "https://sts.us-east-1.amazonaws.com"
		}
	}`
	konnectWIF := func(t *testing.T, req any) string {
		t.Helper()
		data, err := json.Marshal(req)
		require.NoError(t, err)
		var decoded struct {
			Config struct {
				Auth struct {
					WorkloadIdentityFederation json.RawMessage `json:"workload_identity_federation"`
				} `json:"auth"`
			} `json:"config"`
		}
		require.NoError(t, json.Unmarshal(data, &decoded))
		return string(decoded.Config.Auth.WorkloadIdentityFederation)
	}

	t.Run("konnect create", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		req, err := newObj().ToCreateAIGatewayModelProviderRequest(t.Context(), cl)
		require.NoError(t, err)
		require.JSONEq(t, wantKonnectWIF, konnectWIF(t, req))
	})

	t.Run("konnect update", func(t *testing.T) {
		t.Parallel()

		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		req, err := newObj().ToUpdateAIGatewayModelProviderRequest(t.Context(), cl)
		require.NoError(t, err)
		require.JSONEq(t, wantKonnectWIF, konnectWIF(t, req))
	})
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
	data, err := spec.marshalAIGWProviderPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var provider aigw.Provider
	require.NoError(t, dec.Decode(&provider))

	// Pin the discriminator, which selectedSDKOpsPayload copies in from the top-level payload.
	require.Equal(t, "openai", provider.Type)
	require.Equal(t, "openai-provider", provider.Name)
}
