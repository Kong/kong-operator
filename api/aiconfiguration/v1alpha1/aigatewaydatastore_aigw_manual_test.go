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

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// TestAIGatewayDatastore_ToAIGWDatastore covers variant selection, secretRef resolution, the
// plugin-shaped config the converter substitutes into the referencing entities, the drop of the
// display and Konnect-only keys, and the rejections that keep a bad datastore from failing the
// whole DB-less push.
func TestAIGatewayDatastore_ToAIGWDatastore(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given, so parallel subtests sharing one instance race.
	newReferencedSecret := func(name, key, value string) *corev1.Secret {
		return &corev1.Secret{
			Name: name, Namespace: "default",
			Data: map[string][]byte{key: []byte(value)},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayDatastore
		objects []runtime.Object
		want    *aigw.Datastore
		wantErr string
	}{
		{
			name: "redis-ce: config kept in plugin shape, display and Konnect-only keys dropped",
			obj: &AIGatewayDatastore{
				Name: "redis-ce", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeRedisCE,
							RedisCE: &RedisCEDatastore{
								Name:        "shared-redis",
								DisplayName: "Shared Redis",
								Description: "the shared cache",
								Labels:      PublicLabels{"app": "test1"},
								ManagedBy:   ManagedBy{"kong-operator": "true"},
								Config: RedisCEDatastoreConfig{
									Host:      "redis.example.com",
									Port:      6379,
									SSL:       "Enabled",
									SSLVerify: "Disabled",
								},
							},
						},
					},
				},
			},
			want: &aigw.Datastore{
				Type: "redis-ce",
				Name: "shared-redis",
				Config: map[string]any{
					"host":       "redis.example.com",
					"port":       6379,
					"ssl":        true,
					"ssl_verify": false,
				},
			},
		},
		{
			name: "redis-ee: password resolved from a Secret, sentinel and cluster nodes flattened",
			obj: &AIGatewayDatastore{
				Name: "redis-ee", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeRedisEE,
							RedisEE: &RedisEEDatastore{
								Name: "shared-redis-ee",
								Config: RedisEEDatastoreConfig{
									Host: "redis-ee.example.com",
									Port: &RedisEEDatastoreConfigPort{
										Type:     RedisEEDatastoreConfigPortTypeVariant1,
										Variant1: new(RedisEEDatastoreConfigPortVariant1(6379)),
									},
									Password: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "redis-password", Key: "password"},
									},
									SentinelNodes: []RedisEEDatastoreConfigSentinelNodes{{Host: "sentinel.example.com", Port: 26379}},
								},
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret("redis-password", "password", "s3cr3t")},
			want: &aigw.Datastore{
				Type: "redis-ee",
				Name: "shared-redis-ee",
				Config: map[string]any{
					"host":           "redis-ee.example.com",
					"port":           6379,
					"password":       "s3cr3t",
					"sentinel_nodes": []any{map[string]any{"host": "sentinel.example.com", "port": 26379}},
				},
			},
		},
		{
			name: "vectordb: password resolved from a Secret",
			obj: &AIGatewayDatastore{
				Name: "vectordb", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeVectorDB,
							VectorDB: &VectorDBDatastore{
								Name: "pgvector",
								Config: VectorDBDatastoreConfig{
									Host:     "pgvector.example.com",
									Database: "kong-pgvector",
									Port:     5432,
									User:     "kong",
									SSL:      "Enabled",
									Password: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "pgvector-password", Key: "password"},
									},
								},
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret("pgvector-password", "password", "pg-s3cr3t")},
			want: &aigw.Datastore{
				Type: "vectordb",
				Name: "pgvector",
				Config: map[string]any{
					"host":     "pgvector.example.com",
					"database": "kong-pgvector",
					"port":     5432,
					"user":     "kong",
					"ssl":      true,
					"password": "pg-s3cr3t",
				},
			},
		},
		{
			name: "token-vault: directory and key versions kept",
			obj: &AIGatewayDatastore{
				Name: "token-vault", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeTokenVault,
							TokenVault: &TokenVaultDatastore{
								Name: "token-vault",
								Config: TokenVaultDatastoreConfig{
									Directory:      "my-directory",
									CurrentVersion: "v1",
									Keys: []TokenVaultDatastoreKey{{
										Version: "v1",
										Key:     "{vault://env/myvault-token-vault-key}",
									}},
								},
							},
						},
					},
				},
			},
			want: &aigw.Datastore{
				Type: "token-vault",
				Name: "token-vault",
				Config: map[string]any{
					"directory":       "my-directory",
					"current_version": "v1",
					"keys":            []any{map[string]any{"version": "v1", "key": "{vault://env/myvault-token-vault-key}"}},
				},
			},
		},
		{
			name: "unset spec.apiSpec rejected",
			obj: &AIGatewayDatastore{
				Name: "no-apispec", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "type naming a variant whose config is absent rejected",
			obj: &AIGatewayDatastore{
				Name: "no-variant", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeRedisCE,
						},
					},
				},
			},
			wantErr: `config payload missing for type "redis-ce"`,
		},
		{
			name: "missing secret",
			obj: &AIGatewayDatastore{
				Name: "missing-secret", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeRedisCE,
							RedisCE: &RedisCEDatastore{
								Name: "missing-secret-redis",
								Config: RedisCEDatastoreConfig{
									Host: "redis.example.com",
									Password: SensitiveDataSource{
										Type:      SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{Name: "does-not-exist", Key: "password"},
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
			name: "cross-namespace secretRef without KongReferenceGrant rejected",
			obj: &AIGatewayDatastore{
				Name: "cross-ns", Namespace: "default",
				Spec: AIGatewayDatastoreSpec{
					APISpec: AIGatewayDatastoreAPISpec{
						AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
							Type: AIGatewayDatastoreConfigTypeRedisCE,
							RedisCE: &RedisCEDatastore{
								Name: "cross-ns-redis",
								Config: RedisCEDatastoreConfig{
									Host: "redis.example.com",
									Password: SensitiveDataSource{
										Type: SensitiveDataSourceTypeSecretRef,
										SecretRef: &SensitiveDataSecretRef{
											Name:      "redis-password",
											Key:       "password",
											Namespace: new("other-namespace"),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWDatastore(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayPolicy_ToAIGWPolicy_DatastoreRefs covers the reference resolution every referring
// converter shares (resolveDatastoreRefs): the document names the datastore by its entity name,
// and a missing or cross-namespace datastore fails the referencing entity.
func TestAIGatewayPolicy_ToAIGWPolicy_DatastoreRefs(t *testing.T) {
	t.Parallel()

	newPolicy := func(ref AIGatewayDatastoreNamespacedRef) *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Name: "policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name:       "rate-limiting",
					Type:       "rate-limiting",
					Datastores: []AIGatewayDatastoreNamespacedRef{ref},
				},
			},
		}
	}

	t.Run("reference resolves to the datastore's entity name", func(t *testing.T) {
		t.Parallel()

		cl := newDatastoreRefTestClient(t, newTestDatastore("default", "datastore-konnect-id"))
		got, err := newPolicy(AIGatewayDatastoreNamespacedRef{Name: "my-datastore"}).ToAIGWPolicy(t.Context(), cl)
		require.NoError(t, err)
		require.Len(t, got.Datastores, 1)
		require.Equal(t, "shared-redis", got.Datastores[0].Name)
	})

	t.Run("missing datastore", func(t *testing.T) {
		t.Parallel()

		cl := newDatastoreRefTestClient(t)
		_, err := newPolicy(AIGatewayDatastoreNamespacedRef{Name: "does-not-exist"}).ToAIGWPolicy(t.Context(), cl)
		require.ErrorContains(t, err, "does-not-exist")
	})

	t.Run("cross-namespace datastore", func(t *testing.T) {
		t.Parallel()

		cl := newDatastoreRefTestClient(t, newTestDatastore("other", "datastore-konnect-id"))
		_, err := newPolicy(AIGatewayDatastoreNamespacedRef{Name: "my-datastore", Namespace: "other"}).ToAIGWPolicy(t.Context(), cl)
		require.ErrorContains(t, err, "cross-namespace reference")
	})
}

// TestAIGatewayDatastore_ToAIGWDatastore_StrictRoundTrip guards against a dropped or renamed
// field: it decodes marshalAIGWDatastorePayload's output with yaml.v3's KnownFields(true), which
// errors on any key aigw.Datastore doesn't recognize — the display and managed_by drops are
// load-bearing for this. See aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayDatastore_ToAIGWDatastore_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayDatastoreAPISpec{
		AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
			Type: AIGatewayDatastoreConfigTypeRedisCE,
			RedisCE: &RedisCEDatastore{
				Name:        "shared-redis",
				DisplayName: "Shared Redis",
				Description: "the shared cache",
				Labels:      PublicLabels{"app": "test1"},
				ManagedBy:   ManagedBy{"kong-operator": "true"},
				Config:      RedisCEDatastoreConfig{Host: "redis.example.com", SSL: "Enabled"},
			},
		},
	}
	data, err := spec.marshalAIGWDatastorePayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var datastore aigw.Datastore
	require.NoError(t, dec.Decode(&datastore))

	require.Equal(t, "redis-ce", datastore.Type)
	require.Equal(t, "shared-redis", datastore.Name)
	require.Equal(t, map[string]any{"host": "redis.example.com", "ssl": true}, datastore.Config)
}
