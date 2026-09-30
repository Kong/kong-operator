package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayPolicy_ToAIGWPolicy covers the inline config passthrough, the Enabled/Disabled
// string-to-bool normalization, the managed_by drop and the config secretRef resolution (whose
// value must appear in the output without being written back into the caller's spec).
func TestAIGatewayPolicy_ToAIGWPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newConfigSecret returns a fresh object per subtest: the fake client's tracker mutates the
	// objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newConfigSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "policy-config", Namespace: "default",
			Data: map[string][]byte{"config": []byte(`{"limit_by_header":"x-api-key"}`)},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayPolicy
		want    *aigw.Policy
		wantErr string
	}{
		{
			name: "inline config, bool normalization, labels kept, managed_by dropped",
			obj: &AIGatewayPolicy{
				Name: "sample-ai-gw-policy", Namespace: "default",
				Spec: AIGatewayPolicySpec{
					APISpec: AIGatewayPolicyAPISpec{
						Condition: new("request.path ^ /v1/"),
						Config: AIGatewayPolicyConfigDataSource{
							Type: SensitiveDataSourceTypeInline,
							Value: &apiextensionsv1.JSON{
								Raw: []byte(`{"limit_by_header":"x-api-key","second_limit":10}`),
							},
						},
						Datastores:  []AIGatewayDatastoreRef{{Name: "my-redis"}},
						DisplayName: "Rate limiting",
						Enabled:     "Enabled",
						Global:      "Disabled",
						Labels:      PublicLabels{"app": "test1", "env": "test"},
						ManagedBy:   ManagedBy{"kong-operator": "true"},
						Name:        "rate-limit-policy",
						Type:        "rate-limiting",
					},
				},
			},
			want: &aigw.Policy{
				Type:        "rate-limiting",
				Name:        "rate-limit-policy",
				DisplayName: "Rate limiting",
				Enabled:     new(true),
				Global:      new(false),
				Condition:   "request.path ^ /v1/",
				Config:      map[string]any{"limit_by_header": "x-api-key", "second_limit": 10},
				Labels:      aigw.Labels{"app": "test1", "env": "test"},
			},
		},
		{
			name: "secretRef config resolved from Secret",
			obj: &AIGatewayPolicy{
				Name: "sample-ai-gw-policy-secret", Namespace: "default",
				Spec: AIGatewayPolicySpec{
					APISpec: AIGatewayPolicyAPISpec{
						Config: AIGatewayPolicyConfigDataSource{
							Type: SensitiveDataSourceTypeSecretRef,
							SecretRef: &SensitiveDataSecretRef{
								Name: "policy-config",
								Key:  "config",
							},
						},
						DisplayName: "Rate limiting",
						Name:        "rate-limit-policy",
						Type:        "rate-limiting",
					},
				},
			},
			want: &aigw.Policy{
				Type:        "rate-limiting",
				Name:        "rate-limit-policy",
				DisplayName: "Rate limiting",
				Config:      map[string]any{"limit_by_header": "x-api-key"},
			},
		},
		{
			name: "cross-namespace config secretRef rejected",
			obj: &AIGatewayPolicy{
				Name: "sample-ai-gw-policy-cross-ns", Namespace: "default",
				Spec: AIGatewayPolicySpec{
					APISpec: AIGatewayPolicyAPISpec{
						Config: AIGatewayPolicyConfigDataSource{
							Type: SensitiveDataSourceTypeSecretRef,
							SecretRef: &SensitiveDataSecretRef{
								Name:      "policy-config",
								Key:       "config",
								Namespace: new("other-namespace"),
							},
						},
						Name: "rate-limit-policy",
						Type: "rate-limiting",
					},
				},
			},
			wantErr: "cross-namespace secretRef",
		},
		{
			name: "missing secret for config secretRef",
			obj: &AIGatewayPolicy{
				Name: "sample-ai-gw-policy-missing-secret", Namespace: "default",
				Spec: AIGatewayPolicySpec{
					APISpec: AIGatewayPolicyAPISpec{
						Config: AIGatewayPolicyConfigDataSource{
							Type: SensitiveDataSourceTypeSecretRef,
							SecretRef: &SensitiveDataSecretRef{
								Name: "does-not-exist",
								Key:  "config",
							},
						},
						Name: "rate-limit-policy",
						Type: "rate-limiting",
					},
				},
			},
			wantErr: "does-not-exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newConfigSecret()).Build()
			got, err := tt.obj.ToAIGWPolicy(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			// aigw.DatastoreRef has no public alias, so datastores are asserted via field
			// access and blanked before the struct comparison.
			if len(tt.obj.Spec.APISpec.Datastores) > 0 {
				require.Len(t, got.Datastores, len(tt.obj.Spec.APISpec.Datastores))
				require.Equal(t, string(tt.obj.Spec.APISpec.Datastores[0].Name), got.Datastores[0].Name)
			}
			got.Datastores = nil
			require.Equal(t, tt.want, got)
		})
	}

	// sdkOpsAPISpec resolves into a copy of the APISpec (config is a value field); pin that the
	// caller's object still holds the secretRef after conversion.
	obj := tests[1].obj
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newConfigSecret()).Build()
	_, err := obj.ToAIGWPolicy(t.Context(), cl)
	require.NoError(t, err)
	require.Nil(t, obj.Spec.APISpec.Config.Value,
		"secretRef must not be resolved in place on the caller's object")
}

// TestAIGatewayPolicy_ToAIGWPolicy_StrictRoundTrip guards against a dropped or renamed field: it
// decodes marshalAIGWPolicyPayload's output with yaml.v3's KnownFields(true), which errors on
// any key aigw.Policy doesn't recognize. See aigatewaymodel_aigw_manual_test.go for the
// rationale.
func TestAIGatewayPolicy_ToAIGWPolicy_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayPolicyAPISpec{
		Condition: new("request.path ^ /v1/"),
		Config: AIGatewayPolicyConfigDataSource{
			Type:  SensitiveDataSourceTypeInline,
			Value: &apiextensionsv1.JSON{Raw: []byte(`{"limit_by_header":"x-api-key"}`)},
		},
		DisplayName: "Rate limiting",
		Enabled:     "Enabled",
		Global:      "Disabled",
		Labels:      PublicLabels{"app": "test1"},
		Name:        "rate-limit-policy",
		Type:        "rate-limiting",
	}
	data, err := spec.marshalAIGWPolicyPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var policy aigw.Policy
	require.NoError(t, dec.Decode(&policy))

	// Pin the Enabled/Disabled normalization itself, not just its absence of error.
	require.Equal(t, "rate-limiting", policy.Type)
	require.NotNil(t, policy.Enabled)
	require.True(t, *policy.Enabled)
	require.NotNil(t, policy.Global)
	require.False(t, *policy.Global)
	require.Equal(t, map[string]any{"limit_by_header": "x-api-key"}, policy.Config)
}
