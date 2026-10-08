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

// TestAIGatewayAuthStrategy_ToAIGWAuthStrategy covers the variant lifting (the Konnect SDK
// payload nests the variant's fields under the discriminator value; aigw.AuthStrategy is
// flat), the managed_by drop and the secretRef resolution (openid-connect clientSecret).
func TestAIGatewayAuthStrategy_ToAIGWAuthStrategy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "oidc-client-secret", Namespace: "default",
			Data: map[string][]byte{"clientSecret": []byte("s3cr3t")},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayAuthStrategy
		objects []runtime.Object
		want    *aigw.AuthStrategy
		wantErr string
	}{
		{
			name: "key-auth lifted to the flat aigw shape, managed_by dropped",
			obj: &AIGatewayAuthStrategy{
				Name: "ai-gw-auth-strategy-key-auth", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
							KeyAuth: &AIGatewayAuthStrategyKeyAuth{
								Name:        "key-auth-strategy-1",
								DisplayName: "Key Auth Strategy 1",
								Labels:      PublicLabels{"app": "test1"},
								ManagedBy:   ManagedBy{"kong-operator": "true"},
								Config: AIGWKeyAuthGeneratedConfig{
									HideCredentials: "Enabled",
								},
							},
						},
					},
				},
			},
			want: &aigw.AuthStrategy{
				Type:        "key-auth",
				Name:        "key-auth-strategy-1",
				DisplayName: "Key Auth Strategy 1",
				Labels:      aigw.Labels{"app": "test1"},
				Config:      map[string]any{"hide_credentials": true},
			},
		},
		{
			name: "openid-connect clientSecret resolved from a Secret",
			obj: &AIGatewayAuthStrategy{
				Name: "ai-gw-auth-strategy-oidc", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeOpenIDConnect,
							OpenIDConnect: &AIGatewayAuthStrategyOpenIDConnect{
								Name:        "my-oidc-provider",
								DisplayName: "My OpenID Connect Identity Provider",
								Config: AIGWOpenIDConnectGeneratedConfig{
									Issuer:          "https://my-idp.example.com/.well-known/openid-configuration",
									CacheTokensSalt: "my-cache-salt",
									ClientID:        []string{"my-client-id"},
									ClientSecret:    []SensitiveDataSource{{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "oidc-client-secret", Key: "clientSecret"}}},
								},
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret()},
			want: &aigw.AuthStrategy{
				Type:        "openid-connect",
				Name:        "my-oidc-provider",
				DisplayName: "My OpenID Connect Identity Provider",
				Config: map[string]any{
					"issuer":            "https://my-idp.example.com/.well-known/openid-configuration",
					"cache_tokens_salt": "my-cache-salt",
					"client_id":         []any{"my-client-id"},
					"client_secret":     []any{"s3cr3t"},
				},
			},
		},
		{
			name: "cross-namespace secretRef rejected",
			obj: &AIGatewayAuthStrategy{
				Name: "ai-gw-auth-strategy-cross-ns", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeOpenIDConnect,
							OpenIDConnect: &AIGatewayAuthStrategyOpenIDConnect{
								Name:        "cross-ns-oidc",
								DisplayName: "Cross NS OIDC",
								Config: AIGWOpenIDConnectGeneratedConfig{
									ClientSecret: []SensitiveDataSource{{
										Type: SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{
											Name:      "oidc-client-secret",
											Key:       "clientSecret",
											Namespace: new("other-namespace"),
										},
									}},
								},
							},
						},
					},
				},
			},
			wantErr: "cross-namespace secretRef",
		},
		{
			name: "empty apiSpec fails",
			obj: &AIGatewayAuthStrategy{
				Name: "ai-gw-auth-strategy-empty", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWAuthStrategy(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayAuthStrategy_ToAIGWAuthStrategy_StrictRoundTrip catches payload keys that
// aigw.AuthStrategy does not recognize: it decodes marshalAIGWAuthStrategyPayload's output
// with yaml.v3's KnownFields(true). The guard covers aigw.AuthStrategy's struct fields only:
// Config is a map, so a renamed or Konnect-only key inside config passes undetected. See
// aigatewaymodel_aigw_manual_test.go for the rationale. Both variants are covered.
func TestAIGatewayAuthStrategy_ToAIGWAuthStrategy_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		spec            *AIGatewayAuthStrategyAPISpec
		wantName        string
		wantDisplayName string
	}{
		{
			name: "key-auth",
			spec: &AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
					Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
					KeyAuth: &AIGatewayAuthStrategyKeyAuth{
						Name:        "key-auth-strategy-1",
						DisplayName: "Key Auth Strategy 1",
					},
				},
			},
			wantName:        "key-auth-strategy-1",
			wantDisplayName: "Key Auth Strategy 1",
		},
		{
			name: "openid-connect",
			spec: &AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
					Type: AIGatewayAuthStrategyConfigTypeOpenIDConnect,
					OpenIDConnect: &AIGatewayAuthStrategyOpenIDConnect{
						Name:        "my-oidc-provider",
						DisplayName: "My OpenID Connect Identity Provider",
						Config: AIGWOpenIDConnectGeneratedConfig{
							Issuer:          "https://my-idp.example.com/.well-known/openid-configuration",
							CacheTokensSalt: "my-cache-salt",
							ClientID:        []string{"my-client-id"},
							ClientSecret:    []SensitiveDataSource{{Type: SensitiveDataSourceTypeInline, Value: new("s3cr3t")}},
						},
					},
				},
			},
			wantName:        "my-oidc-provider",
			wantDisplayName: "My OpenID Connect Identity Provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := marshalAIGWAuthStrategyPayload(tt.spec)
			require.NoError(t, err)

			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			var strategy aigw.AuthStrategy
			require.NoError(t, dec.Decode(&strategy))

			require.Equal(t, tt.name, strategy.Type)
			require.Equal(t, tt.wantName, strategy.Name)
			require.Equal(t, tt.wantDisplayName, strategy.DisplayName)
		})
	}
}
