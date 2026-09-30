package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// parseAuthStrategy bridges MarshalAIGWAuthStrategy's payload bytes through the same
// single-key-envelope aigw.Parse the translator uses (aigw.AuthStrategy has no public alias,
// so the entity can only be reached through a parsed document's fields).
func parseAuthStrategy(t *testing.T, payload []byte) *aigw.Document {
	t.Helper()
	envelope, err := json.Marshal(map[string][]json.RawMessage{"auth_strategies": {json.RawMessage(payload)}})
	require.NoError(t, err)
	doc, err := aigw.Parse(envelope)
	require.NoError(t, err)
	require.Len(t, doc.AuthStrategies, 1)
	return doc
}

// TestAIGatewayAuthStrategy_MarshalAIGWAuthStrategy covers both union variants, the
// Enabled/Disabled string-to-bool normalization, the managed_by drop and the clientSecret
// secretRef resolution (whose value must appear in the output without being written back into
// the caller's spec).
func TestAIGatewayAuthStrategy_MarshalAIGWAuthStrategy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newOIDCSecret returns a fresh object per subtest: the fake client's tracker mutates the
	// objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newOIDCSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "oidc-creds", Namespace: "default",
			Data: map[string][]byte{"client-secret": []byte("hunter2")},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayAuthStrategy
		wantErr string
		want    func(t *testing.T, doc *aigw.Document)
	}{
		{
			name: "key-auth, bool normalization, managed_by dropped",
			obj: &AIGatewayAuthStrategy{
				Name: "sample-ai-gw-auth-strategy", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
							KeyAuth: &AIGatewayAuthStrategyKeyAuth{
								Name:        "key-auth-strategy",
								DisplayName: "Key Auth",
								Config: AIGatewayAuthStrategyKeyAuthConfig{
									HideCredentials: "Enabled",
								},
								Labels:    PublicLabels{"app": "test1"},
								ManagedBy: ManagedBy{"kong-operator": "true"},
							},
						},
					},
				},
			},
			want: func(t *testing.T, doc *aigw.Document) {
				strategy := doc.AuthStrategies[0]
				require.Equal(t, "key-auth", strategy.Type)
				require.Equal(t, "key-auth-strategy", strategy.Name)
				require.Equal(t, "Key Auth", strategy.DisplayName)
				require.Equal(t, map[string]any{"hide_credentials": true}, strategy.Config)
				require.Equal(t, aigw.Labels{"app": "test1"}, strategy.Labels)
			},
		},
		{
			name: "openid-connect, secretRef clientSecret resolved, auth union flattened",
			obj: &AIGatewayAuthStrategy{
				Name: "sample-ai-gw-auth-strategy-oidc", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeOpenIDConnect,
							OpenIDConnect: &AIGatewayAuthStrategyOpenIDConnect{
								Name:        "oidc-strategy",
								DisplayName: "OIDC",
								Config: AIGatewayAuthStrategyOpenIDConnectConfig{
									ClientSecret: []SensitiveDataSource{
										{
											Type: SensitiveDataSourceTypeSecretRef,
											SecretRef: &SensitiveDataSecretRef{
												Name: "oidc-creds",
												Key:  "client-secret",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: func(t *testing.T, doc *aigw.Document) {
				strategy := doc.AuthStrategies[0]
				require.Equal(t, "openid-connect", strategy.Type)
				require.Equal(t, "oidc-strategy", strategy.Name)
				require.Equal(t, "OIDC", strategy.DisplayName)
				require.Contains(t, strategy.Config, "client_secret")
				require.Equal(t, []any{"hunter2"}, strategy.Config["client_secret"])
			},
		},
		{
			name: "cross-namespace clientSecret secretRef rejected",
			obj: &AIGatewayAuthStrategy{
				Name: "sample-ai-gw-auth-strategy-cross-ns", Namespace: "default",
				Spec: AIGatewayAuthStrategySpec{
					APISpec: AIGatewayAuthStrategyAPISpec{
						AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
							Type: AIGatewayAuthStrategyConfigTypeOpenIDConnect,
							OpenIDConnect: &AIGatewayAuthStrategyOpenIDConnect{
								Name:        "oidc-strategy",
								DisplayName: "OIDC",
								Config: AIGatewayAuthStrategyOpenIDConnectConfig{
									ClientSecret: []SensitiveDataSource{
										{
											Type: SensitiveDataSourceTypeSecretRef,
											SecretRef: &SensitiveDataSecretRef{
												Name:      "oidc-creds",
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
			wantErr: "cross-namespace secretRef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newOIDCSecret()).Build()
			payload, err := tt.obj.MarshalAIGWAuthStrategy(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.want(t, parseAuthStrategy(t, payload))
		})
	}

	// sdkOpsAPISpec writes resolved values back into the spec it walks; MarshalAIGWAuthStrategy
	// must do that on a copy. Pin that the caller's object still holds the secretRef after
	// conversion.
	obj := tests[1].obj
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newOIDCSecret()).Build()
	_, err := obj.MarshalAIGWAuthStrategy(t.Context(), cl)
	require.NoError(t, err)
	require.Empty(t, obj.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].GetValue(),
		"secretRef must not be resolved in place on the caller's object")
}

// TestAIGatewayAuthStrategy_MarshalAIGWAuthStrategy_RoundTrip decodes the production payload
// through the same envelope Parse the translator uses and pins the full field set. Unlike the
// sibling kinds this can't use yaml KnownFields(true) — aigw.AuthStrategy has no public alias
// to decode strictly into — so the full-field assertions are the drift guard: a dropped or
// renamed field shows up as a missing/wrong value here.
func TestAIGatewayAuthStrategy_MarshalAIGWAuthStrategy_RoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayAuthStrategyAPISpec{
		AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
			Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
			KeyAuth: &AIGatewayAuthStrategyKeyAuth{
				Name:        "key-auth-strategy",
				DisplayName: "Key Auth",
				Config:      AIGatewayAuthStrategyKeyAuthConfig{HideCredentials: "Enabled"},
			},
		},
	}
	payload, err := spec.marshalAIGWAuthStrategyPayload()
	require.NoError(t, err)

	doc := parseAuthStrategy(t, payload)
	strategy := doc.AuthStrategies[0]
	require.Equal(t, "key-auth", strategy.Type)
	require.Equal(t, "key-auth-strategy", strategy.Name)
	require.Equal(t, "Key Auth", strategy.DisplayName)
	require.Equal(t, map[string]any{"hide_credentials": true}, strategy.Config)
}
