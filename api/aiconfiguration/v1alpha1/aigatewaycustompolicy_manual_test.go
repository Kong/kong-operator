package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func inlineConfigMapDataSource(value string) ConfigMapDataSource {
	return ConfigMapDataSource{Type: ConfigMapDataSourceTypeInline, Value: new(value)}
}

func configMapRefDataSource(name, key string) ConfigMapDataSource {
	return ConfigMapDataSource{
		Type:         ConfigMapDataSourceTypeConfigMapRef,
		ConfigMapRef: &ConfigMapDataSourceRef{Name: name, Key: key},
	}
}

// TestAIGatewayCustomPolicyAPISpec_LabelsAndManagedByReachSDKVerbatim checks
// that labels and managedBy, set on either union variant, are carried into the
// SDK create/update requests with their user-provided keys untouched (i.e. not
// camelCase→snake_case renamed or collapsed by the union flattening).
func TestAIGatewayCustomPolicyAPISpec_LabelsAndManagedByReachSDKVerbatim(t *testing.T) {
	labels := PublicLabels{"teamName": "ai", "cost-center": "1234"}
	managedBy := ManagedBy{"toolName": "kong-operator", "env": "prod"}
	wantLabels := map[string]string{"teamName": "ai", "cost-center": "1234"}
	wantManagedBy := map[string]string{"toolName": "kong-operator", "env": "prod"}

	t.Run("installed", func(t *testing.T) {
		spec := &AIGatewayCustomPolicyAPISpec{
			AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
				Type: AIGatewayCustomPolicyConfigTypeInstalled,
				Installed: &CreateAIGatewayCustomPolicyInstalledRequest{
					Name:        "my-installed-policy",
					DisplayName: "My installed policy",
					Schema:      inlineConfigMapDataSource("return {}"),
					Labels:      labels,
					ManagedBy:   managedBy,
				},
			},
		}

		createReq, err := spec.ToCreateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, createReq.CreateAIGatewayCustomPolicyInstalledRequest)
		assert.Equal(t, wantLabels, createReq.CreateAIGatewayCustomPolicyInstalledRequest.GetLabels())
		assert.Equal(t, wantManagedBy, createReq.CreateAIGatewayCustomPolicyInstalledRequest.GetManagedBy())

		updateReq, err := spec.ToUpdateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest)
		assert.Equal(t, wantLabels, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest.GetLabels())
		assert.Equal(t, wantManagedBy, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest.GetManagedBy())
	})

	t.Run("streaming", func(t *testing.T) {
		spec := &AIGatewayCustomPolicyAPISpec{
			AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
				Type: AIGatewayCustomPolicyConfigTypeStreaming,
				Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
					Name:        "my-streaming-policy",
					DisplayName: "My streaming policy",
					Schema:      inlineConfigMapDataSource("return {}"),
					Handler:     inlineConfigMapDataSource("return {}"),
					Labels:      labels,
					ManagedBy:   managedBy,
				},
			},
		}

		createReq, err := spec.ToCreateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, createReq.CreateAIGatewayCustomPolicyStreamingRequest)
		assert.Equal(t, wantLabels, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetLabels())
		assert.Equal(t, wantManagedBy, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetManagedBy())

		updateReq, err := spec.ToUpdateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest)
		assert.Equal(t, wantLabels, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetLabels())
		assert.Equal(t, wantManagedBy, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetManagedBy())
	})
}

// TestAIGatewayCustomPolicy_LuaSourcesFromConfigMap checks that Lua sources
// referenced via configMapRef are resolved from the ConfigMap (data or
// binaryData) in the object's namespace before reaching the SDK request, and
// that inline sources are passed through unchanged.
func TestAIGatewayCustomPolicy_LuaSourcesFromConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	const ns = "default"
	luaConfigMap := &corev1.ConfigMap{
		Name: "custom-policy-lua", Namespace: ns,
		Data: map[string]string{
			"schema.lua": "return { name = \"from-configmap\" }",
		},
		BinaryData: map[string][]byte{
			"handler.lua": []byte("return { VERSION = \"1.0.0\" }"),
		},
	}
	otherNamespaceConfigMap := luaConfigMap.DeepCopy()
	otherNamespaceConfigMap.Namespace = "other"
	otherNamespaceConfigMap.Name = "other-ns-lua"

	streamingPolicy := func(schema, handler ConfigMapDataSource) *AIGatewayCustomPolicy {
		return &AIGatewayCustomPolicy{
			Name: "policy", Namespace: ns,
			Spec: AIGatewayCustomPolicySpec{
				APISpec: AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
						Type: AIGatewayCustomPolicyConfigTypeStreaming,
						Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
							Name:        "my-streaming-policy",
							DisplayName: "My streaming policy",
							Schema:      schema,
							Handler:     handler,
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name        string
		obj         *AIGatewayCustomPolicy
		clientObjs  []client.Object
		wantErr     string
		wantSchema  string
		wantHandler string
		wantRefs    []ConfigMapDataSourceRef
	}{
		{
			name: "inline sources are passed through",
			obj: streamingPolicy(
				inlineConfigMapDataSource("return {}"),
				inlineConfigMapDataSource("return { VERSION = \"0.1.0\" }"),
			),
			wantSchema:  "return {}",
			wantHandler: "return { VERSION = \"0.1.0\" }",
		},
		{
			name: "sources are resolved from ConfigMap data and binaryData",
			obj: streamingPolicy(
				configMapRefDataSource("custom-policy-lua", "schema.lua"),
				configMapRefDataSource("custom-policy-lua", "handler.lua"),
			),
			clientObjs:  []client.Object{luaConfigMap},
			wantSchema:  "return { name = \"from-configmap\" }",
			wantHandler: "return { VERSION = \"1.0.0\" }",
			wantRefs: []ConfigMapDataSourceRef{
				{Name: "custom-policy-lua", Key: "schema.lua"},
				{Name: "custom-policy-lua", Key: "handler.lua"},
			},
		},
		{
			name: "inline and ConfigMap sources can be mixed",
			obj: streamingPolicy(
				inlineConfigMapDataSource("return {}"),
				configMapRefDataSource("custom-policy-lua", "handler.lua"),
			),
			clientObjs:  []client.Object{luaConfigMap},
			wantSchema:  "return {}",
			wantHandler: "return { VERSION = \"1.0.0\" }",
			wantRefs: []ConfigMapDataSourceRef{
				{Name: "custom-policy-lua", Key: "handler.lua"},
			},
		},
		{
			name: "missing ConfigMap returns an error",
			obj: streamingPolicy(
				configMapRefDataSource("missing", "schema.lua"),
				inlineConfigMapDataSource("return {}"),
			),
			wantErr:  "failed to fetch ConfigMap default/missing",
			wantRefs: []ConfigMapDataSourceRef{{Name: "missing", Key: "schema.lua"}},
		},
		{
			name: "ConfigMap in another namespace is not used",
			obj: streamingPolicy(
				configMapRefDataSource("other-ns-lua", "schema.lua"),
				inlineConfigMapDataSource("return {}"),
			),
			clientObjs: []client.Object{otherNamespaceConfigMap},
			wantErr:    "failed to fetch ConfigMap default/other-ns-lua",
			wantRefs:   []ConfigMapDataSourceRef{{Name: "other-ns-lua", Key: "schema.lua"}},
		},
		{
			name: "missing key returns an error",
			obj: streamingPolicy(
				inlineConfigMapDataSource("return {}"),
				configMapRefDataSource("custom-policy-lua", "missing.lua"),
			),
			clientObjs: []client.Object{luaConfigMap},
			wantErr:    `configmap default/custom-policy-lua is missing key "missing.lua"`,
			wantRefs:   []ConfigMapDataSourceRef{{Name: "custom-policy-lua", Key: "missing.lua"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.clientObjs...).Build()

			assert.Equal(t, tc.wantRefs, tc.obj.GetConfigMapDataSourceRefs())

			createReq, err := tc.obj.ToCreateAIGatewayCustomPolicyRequest(t.Context(), cl)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, createReq.CreateAIGatewayCustomPolicyStreamingRequest)
			assert.Equal(t, tc.wantSchema, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetSchema())
			assert.Equal(t, tc.wantHandler, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetHandler())

			updateReq, err := tc.obj.ToUpdateAIGatewayCustomPolicyRequest(t.Context(), cl)
			require.NoError(t, err)
			require.NotNil(t, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest)
			assert.Equal(t, tc.wantSchema, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetSchema())
			assert.Equal(t, tc.wantHandler, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetHandler())

			// Resolution must not mutate the stored object.
			assert.Equal(t, tc.obj.Spec.APISpec.Streaming.Schema.Type == ConfigMapDataSourceTypeConfigMapRef, tc.obj.Spec.APISpec.Streaming.Schema.Value == nil)
		})
	}
}
