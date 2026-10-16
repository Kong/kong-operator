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

// TestAIGatewayVault_ToAIGWVault covers variant selection, the nested hcv authMethod union
// flattening, secretRef resolution, the managed_by drop, and the rejections that keep a bad
// vault from failing the whole DB-less push.
func TestAIGatewayVault_ToAIGWVault(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedSecret := func(name, key, value string) *corev1.Secret {
		return &corev1.Secret{
			Name: name, Namespace: "default",
			Data: map[string][]byte{key: []byte(value)},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayVault
		objects []runtime.Object
		want    *aigw.Vault
		wantErr string
	}{
		{
			name: "env vault: name becomes the prefix, type the backend, labels kept, managed_by dropped",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-env", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeEnvironmentVariable,
							EnvironmentVariable: &EnvironmentVariableVault{
								Name:      "env",
								Labels:    PublicLabels{"app": "test1"},
								ManagedBy: ManagedBy{"kong-operator": "true"},
								Config:    EnvironmentVariableVaultConfig{Prefix: "MY_SECRET_"},
							},
						},
					},
				},
			},
			want: &aigw.Vault{
				Type:   "env",
				Name:   "env",
				Config: map[string]any{"prefix": "MY_SECRET_"},
				Labels: aigw.Labels{"app": "test1"},
			},
		},
		{
			name: "aws vault",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-aws", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeAwsSecretsManager,
							AwsSecretsManager: &AwsSecretsManagerVault{
								Name: "aws",
								Config: AwsSecretsManagerVaultConfig{
									Region:          "eu-central-1",
									RoleSessionName: "my-session",
								},
							},
						},
					},
				},
			},
			want: &aigw.Vault{
				Type: "aws",
				Name: "aws",
				Config: map[string]any{
					"region":            "eu-central-1",
					"role_session_name": "my-session",
				},
			},
		},
		{
			name: "conjur apiKey resolved from a Secret",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-conjur", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeConjur,
							Conjur: &ConjurVault{
								Name: "conjur",
								Config: ConjurVaultConfig{
									Account: "my-org",
									Login:   "my-workload",
									APIKey: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "conjur-apikey", Key: "apiKey"},
									},
								},
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret("conjur-apikey", "apiKey", "the-api-key")},
			want: &aigw.Vault{
				Type: "conjur",
				Name: "conjur",
				Config: map[string]any{
					"account": "my-org",
					"login":   "my-workload",
					"api_key": "the-api-key",
				},
			},
		},
		{
			// The hcv config is itself a union (authMethod + the variant named after it), so
			// this pins that it flattens to Kong's flat config shape (auth_method alongside
			// the token's own fields) rather than nesting the token payload under "token".
			name: "hcv token authMethod union flattened, token resolved from a Secret",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-hcv", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeHashiCorp,
							HashiCorp: &HashiCorpVault{
								Name: "hcv",
								Config: &HashiCorpVaultConfig{
									AuthMethod: HashiCorpVaultConfigTypeToken,
									Token: &HashiCorpVaultTokenConfig{
										Host:  "vault.example.com",
										Mount: "secret",
										Token: SensitiveDataSource{
											Type:      SensitiveDataSourceTypeSecretRef,
											SecretRef: &SensitiveDataSecretRef{Name: "hcv-token", Key: "token"},
										},
									},
								},
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret("hcv-token", "token", "s3cr3t")},
			want: &aigw.Vault{
				Type: "hcv",
				Name: "hcv",
				Config: map[string]any{
					"auth_method": "token",
					"host":        "vault.example.com",
					"mount":       "secret",
					"token":       "s3cr3t",
				},
			},
		},
		{
			name: "konnect config store vault rejected",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-konnect", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeKonnectConfigStore,
							KonnectConfigStore: &KonnectConfigStoreVault{
								Name:   "konnect",
								Config: KonnectConfigStoreVaultConfig{ConfigStoreID: "store-id"},
							},
						},
					},
				},
			},
			wantErr: `type "konnect" is not supported on-prem`,
		},
		{
			name: "unset spec.apiSpec rejected",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-no-apispec", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "cross-namespace secretRef rejected",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-cross-ns", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeConjur,
							Conjur: &ConjurVault{
								Name: "cross-ns-conjur",
								Config: ConjurVaultConfig{
									APIKey: SensitiveDataSource{
										Type: SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{
											Name:      "conjur-apikey",
											Key:       "apiKey",
											Namespace: new("other-namespace"),
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
		{
			name: "missing secret",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-missing-secret", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeConjur,
							Conjur: &ConjurVault{
								Name: "missing-secret-conjur",
								Config: ConjurVaultConfig{
									APIKey: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "does-not-exist", Key: "apiKey"},
									},
								},
							},
						},
					},
				},
			},
			wantErr: "does-not-exist",
		},
		{
			name: "type naming a variant whose config is absent rejected",
			obj: &AIGatewayVault{
				Name: "sample-ai-gw-vault-no-variant", Namespace: "default",
				Spec: AIGatewayVaultSpec{
					APISpec: AIGatewayVaultAPISpec{
						AIGatewayVaultConfig: &AIGatewayVaultConfig{
							Type: AIGatewayVaultConfigTypeEnvironmentVariable,
						},
					},
				},
			},
			wantErr: `config payload missing for type "env"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWVault(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayVault_ToAIGWVault_StrictRoundTrip guards against a dropped or renamed field: it
// decodes marshalAIGWVaultPayload's output with yaml.v3's KnownFields(true), which errors on
// any key aigw.Vault doesn't recognize — the managed_by drop is load-bearing for this. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayVault_ToAIGWVault_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayVaultAPISpec{
		AIGatewayVaultConfig: &AIGatewayVaultConfig{
			Type: AIGatewayVaultConfigTypeEnvironmentVariable,
			EnvironmentVariable: &EnvironmentVariableVault{
				Name:      "env",
				Labels:    PublicLabels{"app": "test1"},
				ManagedBy: ManagedBy{"kong-operator": "true"},
				Config:    EnvironmentVariableVaultConfig{Prefix: "MY_SECRET_"},
			},
		},
	}
	data, err := spec.marshalAIGWVaultPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var vault aigw.Vault
	require.NoError(t, dec.Decode(&vault))

	require.Equal(t, "env", vault.Name)
	require.Equal(t, "env", vault.Type)
	require.Equal(t, map[string]any{"prefix": "MY_SECRET_"}, vault.Config)
	require.Equal(t, aigw.Labels{"app": "test1"}, vault.Labels)
}
