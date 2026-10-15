package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// TestAIGatewayPolicy_ToAIGWPolicy_CustomPolicyRef covers the on-prem
// customPolicyRef resolution: the referenced custom policy's Konnect name
// becomes the policy's type, and the error branches (type + customPolicyRef
// mutual exclusion, missing custom policy, cross-namespace ref, nameless
// custom policy, custom policy bound to a different AI Gateway) fail with
// actionable messages instead of rendering a nameless plugin.
func TestAIGatewayPolicy_ToAIGWPolicy_CustomPolicyRef(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// customPolicy returns a streaming AIGatewayCustomPolicy named name,
	// bound to the on-prem gateway gwName, in the default namespace.
	customPolicy := func(name, gwName string) *AIGatewayCustomPolicy {
		return &AIGatewayCustomPolicy{
			Namespace: "default", Name: name,
			Spec: AIGatewayCustomPolicySpec{
				AIGatewayRef: AIGatewayRef{
					Group:         AIGatewayRefGroupOnPrem,
					Kind:          AIGatewayRefKindOnPrem,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gwName},
				},
				APISpec: AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
						Type: AIGatewayCustomPolicyConfigTypeStreaming,
						Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
							Name: AIGatewayEntityIdentifier(name),
							Schema: ConfigMapDataSource{
								Type:  ConfigMapDataSourceTypeInline,
								Value: new(`return { name = "` + name + `" }`),
							},
							Handler: ConfigMapDataSource{
								Type:  ConfigMapDataSourceTypeInline,
								Value: new(`return {}`),
							},
						},
					},
				},
			},
		}
	}

	// policy returns an AIGatewayPolicy in the default namespace bound to gw,
	// with Type and customPolicyRef set according to the arguments.
	policy := func(typ string, ref *AIGatewayCustomPolicyRef) *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Namespace: "default", Name: "sample-policy",
			Spec: AIGatewayPolicySpec{
				AIGatewayRef: AIGatewayRef{
					Group:         AIGatewayRefGroupOnPrem,
					Kind:          AIGatewayRefKindOnPrem,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
				},
				APISpec: AIGatewayPolicyAPISpec{
					Name:            "sample-policy",
					DisplayName:     "Sample policy",
					Type:            typ,
					CustomPolicyRef: *ref,
				},
			},
		}
	}
	refTo := func(name, namespace string) *AIGatewayCustomPolicyRef {
		return &AIGatewayCustomPolicyRef{Name: name, Namespace: namespace}
	}
	builderWith := func(objs ...client.Object) *fake.ClientBuilder {
		builder := fake.NewClientBuilder().WithScheme(scheme)
		for _, obj := range objs {
			builder = builder.WithObjects(obj)
		}
		return builder
	}

	tests := []struct {
		name    string
		obj     *AIGatewayPolicy
		objects []client.Object
		wantTyp string
		wantErr string
	}{
		{
			name:    "customPolicyRef resolves to the custom policy's Konnect name",
			obj:     policy("", refTo("cp", "")),
			objects: []client.Object{customPolicy("cp", "gw")},
			wantTyp: "cp",
		},
		{
			name:    "type and customPolicyRef are mutually exclusive",
			obj:     policy("rate-limiting", refTo("cp", "")),
			objects: []client.Object{customPolicy("cp", "gw")},
			wantErr: "type and customPolicyRef are mutually exclusive",
		},
		{
			name:    "missing custom policy fails",
			obj:     policy("", refTo("missing", "")),
			wantErr: "resolving AIGatewayPolicy default/sample-policy customPolicyRef \"missing\"",
		},
		{
			name:    "cross-namespace customPolicyRef fails",
			obj:     policy("", refTo("cp", "other")),
			objects: []client.Object{customPolicy("cp", "gw")},
			wantErr: "cross-namespace customPolicyRef other/cp is not supported on-prem",
		},
		{
			name: "custom policy without apiSpec fails",
			obj:  policy("", refTo("nameless", "")),
			objects: []client.Object{&AIGatewayCustomPolicy{
				Namespace: "default", Name: "nameless",
			}},
			wantErr: "referenced custom policy \"nameless\" has no name set",
		},
		{
			name:    "custom policy bound to a different AI Gateway fails",
			obj:     policy("", refTo("cp", "")),
			objects: []client.Object{customPolicy("cp", "other-gw")},
			wantErr: "customPolicyRef \"cp\" belongs to a different AI Gateway",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.obj.ToAIGWPolicy(t.Context(), builderWith(tc.objects...).Build())
			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTyp, got.Type)
		})
	}
}
