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

// TestAIGatewayCustomPolicy_ToAIGWCustomPolicy covers the installed/streaming union
// flattening, the ConfigMapDataSource resolution (inline and configMapRef, whose value must
// appear in the output without being written back into the caller's spec) and the drops of
// the fields aigw.CustomPolicy has no equivalent for.
func TestAIGatewayCustomPolicy_ToAIGWCustomPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newSchemaConfigMap returns a fresh object per subtest: the fake client's tracker mutates
	// the objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newSchemaConfigMap := func() *corev1.ConfigMap {
		return &corev1.ConfigMap{
			Name: "policy-schema", Namespace: "default",
			Data: map[string]string{"schema.lua": "return { name = \"my-policy\" }"},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayCustomPolicy
		want    *aigw.CustomPolicy
		wantErr string
	}{
		{
			name: "installed with inline schema, display_name/labels/managed_by dropped",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{
					APISpec: AIGatewayCustomPolicyAPISpec{
						AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
							Type: AIGatewayCustomPolicyConfigTypeInstalled,
							Installed: &CreateAIGatewayCustomPolicyInstalledRequest{
								Name:        "my-installed-policy",
								DisplayName: "My Installed Policy",
								Labels:      PublicLabels{"app": "test1"},
								ManagedBy:   ManagedBy{"kong-operator": "true"},
								Schema:      inlineConfigMapDataSource("return { name = \"my-policy\" }"),
							},
						},
					},
				},
			},
			want: &aigw.CustomPolicy{
				Type:   "installed",
				Name:   "my-installed-policy",
				Schema: "return { name = \"my-policy\" }",
			},
		},
		{
			name: "installed with configMapRef schema resolved from ConfigMap",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy-configmap", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{
					APISpec: AIGatewayCustomPolicyAPISpec{
						AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
							Type: AIGatewayCustomPolicyConfigTypeInstalled,
							Installed: &CreateAIGatewayCustomPolicyInstalledRequest{
								Name:   "my-installed-policy",
								Schema: configMapRefDataSource("policy-schema", "schema.lua"),
							},
						},
					},
				},
			},
			want: &aigw.CustomPolicy{
				Type:   "installed",
				Name:   "my-installed-policy",
				Schema: "return { name = \"my-policy\" }",
			},
		},
		{
			name: "streaming with inline schema and configMapRef handler",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy-streaming", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{
					APISpec: AIGatewayCustomPolicyAPISpec{
						AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
							Type: AIGatewayCustomPolicyConfigTypeStreaming,
							Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
								Name:    "my-streaming-policy",
								Schema:  inlineConfigMapDataSource("return { name = \"my-policy\" }"),
								Handler: configMapRefDataSource("policy-schema", "schema.lua"),
							},
						},
					},
				},
			},
			want: &aigw.CustomPolicy{
				Type:    "streaming",
				Name:    "my-streaming-policy",
				Schema:  "return { name = \"my-policy\" }",
				Handler: "return { name = \"my-policy\" }",
			},
		},
		{
			name: "nil spec.apiSpec config rejected",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy-nil", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{},
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "variant payload missing for the selected type",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy-mismatch", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{
					APISpec: AIGatewayCustomPolicyAPISpec{
						AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
							Type: AIGatewayCustomPolicyConfigTypeInstalled,
							// installed selected, but only the streaming variant is populated.
							Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
								Name:   "my-streaming-policy",
								Schema: inlineConfigMapDataSource("return { name = \"my-policy\" }"),
							},
						},
					},
				},
			},
			wantErr: "config payload missing",
		},
		{
			name: "missing key in referenced ConfigMap",
			obj: &AIGatewayCustomPolicy{
				Name: "sample-ai-gw-custom-policy-missing-key", Namespace: "default",
				Spec: AIGatewayCustomPolicySpec{
					APISpec: AIGatewayCustomPolicyAPISpec{
						AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
							Type: AIGatewayCustomPolicyConfigTypeInstalled,
							Installed: &CreateAIGatewayCustomPolicyInstalledRequest{
								Name:   "my-installed-policy",
								Schema: configMapRefDataSource("policy-schema", "no-such-key"),
							},
						},
					},
				},
			},
			wantErr: "missing key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSchemaConfigMap()).Build()
			got, err := tt.obj.ToAIGWCustomPolicy(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	// sdkOpsAPISpec resolves into a deep copy of the APISpec; pin that the caller's object
	// still holds the configMapRef after conversion.
	obj := tests[1].obj
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSchemaConfigMap()).Build()
	_, err := obj.ToAIGWCustomPolicy(t.Context(), cl)
	require.NoError(t, err)
	require.Nil(t, obj.Spec.APISpec.Installed.Schema.Value,
		"configMapRef must not be resolved in place on the caller's object")
}

// TestAIGatewayCustomPolicy_ToAIGWCustomPolicy_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWCustomPolicyPayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.CustomPolicy doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayCustomPolicy_ToAIGWCustomPolicy_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayCustomPolicyAPISpec{
		AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
			Type: AIGatewayCustomPolicyConfigTypeStreaming,
			Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
				Name:        "my-streaming-policy",
				DisplayName: "My Streaming Policy",
				Labels:      PublicLabels{"app": "test1"},
				ManagedBy:   ManagedBy{"kong-operator": "true"},
				Schema:      inlineConfigMapDataSource("return { name = \"my-policy\" }"),
				Handler:     inlineConfigMapDataSource("local kong = kong"),
			},
		},
	}
	data, err := spec.marshalAIGWCustomPolicyPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var policy aigw.CustomPolicy
	require.NoError(t, dec.Decode(&policy))

	require.Equal(t, aigw.CustomPolicy{
		Type:    "streaming",
		Name:    "my-streaming-policy",
		Schema:  "return { name = \"my-policy\" }",
		Handler: "local kong = kong",
	}, policy)
}
