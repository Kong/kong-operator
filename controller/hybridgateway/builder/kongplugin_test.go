package builder

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestNewKongPlugin(t *testing.T) {
	builder := NewKongPlugin()

	assert.NotNil(t, builder)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1.KongPlugin{}, builder.plugin)
}

func TestKongPluginBuilder_WithName(t *testing.T) {
	builder := NewKongPlugin().WithName("test-plugin")

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", plugin.Name)
}

func TestKongPluginBuilder_WithNamespace(t *testing.T) {
	builder := NewKongPlugin().WithNamespace("test-namespace")

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", plugin.Namespace)
}

func TestKongPluginBuilder_WithLabels(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	builder := NewKongPlugin().WithLabels(route)

	plugin, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, plugin.Labels)
	assert.NotEmpty(t, plugin.Labels)
}

func TestKongPluginBuilder_WithAnnotations(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPlugin().WithAnnotations(route, parentRef)

	plugin, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, plugin.Annotations)
	assert.NotEmpty(t, plugin.Annotations)
}

func TestKongPluginBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongPlugin().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		plugin, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, plugin.OwnerReferences, 1)
		ownerRef := plugin.OwnerReferences[0]
		assert.Equal(t, "HTTPRoute", ownerRef.Kind)
		assert.Equal(t, "gateway.networking.k8s.io/v1", ownerRef.APIVersion)
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongPlugin().WithOwner(nil)

		_, err := builder.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})

	t.Run("owner reference error", func(t *testing.T) {
		builder := NewKongPlugin().
			WithNamespace("wrong-namespace").
			WithOwner(httpRoute)
		_, err := builder.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongPluginBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongPlugin().WithName("test-plugin")

		plugin := builder.MustBuild()
		assert.Equal(t, "test-plugin", plugin.Name)
	})

	t.Run("must build panics on error", func(t *testing.T) {
		builder := NewKongPlugin().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongPluginBuilder_WithFilter_RequestHeaderModifier(t *testing.T) {
	tests := []struct {
		name           string
		filter         gwtypes.HTTPRouteFilter
		expectedPlugin string
		expectedConfig requestTransformer
		expectError    bool
	}{
		{
			name: "add headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &v1.HTTPHeaderFilter{
					Add: []v1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
						{Name: "X-Another-Header", Value: "another-value"},
					},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: requestTransformer{
				Add: requestTransformerTargetSlice{
					Headers: []string{"X-Custom-Header:custom-value", "X-Another-Header:another-value"},
				},
				Remove: requestTransformerTargetSlice{
					Headers: []string{},
				},
			},
		},
		{
			name: "remove headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &v1.HTTPHeaderFilter{
					Remove: []string{"X-Remove-Header", "custom-value"},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: requestTransformer{
				Add: requestTransformerTargetSlice{
					Headers: []string{},
				},
				Remove: requestTransformerTargetSlice{
					Headers: []string{"X-Remove-Header", "custom-value"},
				},
			},
		},
		{
			name: "set headers (remove + add)",
			filter: gwtypes.HTTPRouteFilter{
				Type: v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &v1.HTTPHeaderFilter{
					Set: []v1.HTTPHeader{
						{Name: "Authorization", Value: "Bearer token123"},
					},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: requestTransformer{
				Add: requestTransformerTargetSlice{
					Headers: []string{"Authorization:Bearer token123"},
				},
				Remove: requestTransformerTargetSlice{
					Headers: []string{"Authorization"},
				},
			},
		},
		{
			name: "mixed operations",
			filter: gwtypes.HTTPRouteFilter{
				Type: v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &v1.HTTPHeaderFilter{
					Add: []v1.HTTPHeader{
						{Name: "X-Add-Header", Value: "add-value"},
					},
					Set: []v1.HTTPHeader{
						{Name: "X-Set-Header", Value: "set-value"},
					},
					Remove: []string{"X-Remove-Header"},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: requestTransformer{
				Add: requestTransformerTargetSlice{
					Headers: []string{"X-Set-Header:set-value", "X-Add-Header:add-value"},
				},
				Remove: requestTransformerTargetSlice{
					Headers: []string{"X-Set-Header", "X-Remove-Header"},
				},
			},
		},
		{
			name: "nil RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                  v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: nil,
			},
			expectError: true,
		},
		{
			name: "empty RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                  v1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &v1.HTTPHeaderFilter{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongPlugin().WithFilter(tt.filter)

			plugin, err := builder.Build()

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedPlugin, plugin.PluginName)

			var actualConfig requestTransformer
			err = json.Unmarshal(plugin.Config.Raw, &actualConfig)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.expectedConfig.Add.Headers, actualConfig.Add.Headers)
			assert.ElementsMatch(t, tt.expectedConfig.Remove.Headers, actualConfig.Remove.Headers)
		})
	}
}

func TestKongPluginBuilder_WithFilter_UnsupportedType(t *testing.T) {
	filter := gwtypes.HTTPRouteFilter{
		Type: v1.HTTPRouteFilterCORS, // Unsupported type
	}

	builder := NewKongPlugin().WithFilter(filter)

	_, err := builder.Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported filter type")
}

func TestTranslateRequestModifier(t *testing.T) {
	tests := []struct {
		name        string
		filter      gwtypes.HTTPRouteFilter
		expected    requestTransformer
		expectError bool
	}{
		{
			name: "successful translation with all operations",
			filter: gwtypes.HTTPRouteFilter{
				RequestHeaderModifier: &v1.HTTPHeaderFilter{
					Add: []v1.HTTPHeader{
						{Name: "X-Add", Value: "add-val"},
					},
					Set: []v1.HTTPHeader{
						{Name: "X-Set", Value: "set-val"},
					},
					Remove: []string{"X-Remove"},
				},
			},
			expected: requestTransformer{
				Add: requestTransformerTargetSlice{
					Headers: []string{"X-Set:set-val", "X-Add:add-val"},
				},
				Remove: requestTransformerTargetSlice{
					Headers: []string{"X-Set", "X-Remove"},
				},
			},
		},
		{
			name: "nil RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				RequestHeaderModifier: nil,
			},
			expectError: true,
		},
		{
			name: "empty RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				RequestHeaderModifier: &v1.HTTPHeaderFilter{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateRequestModifier(tt.filter)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, requestTransformer{}, result)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.expected.Add.Headers, result.Add.Headers)
			assert.ElementsMatch(t, tt.expected.Remove.Headers, result.Remove.Headers)
		})
	}
}

func TestKongPluginBuilder_ChainedCalls(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	filter := gwtypes.HTTPRouteFilter{
		Type: v1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &v1.HTTPHeaderFilter{
			Add: []v1.HTTPHeader{
				{Name: "X-Test", Value: "test-value"},
			},
		},
	}

	plugin := NewKongPlugin().
		WithName("test-plugin").
		WithNamespace("test-ns").
		WithLabels(route).
		WithAnnotations(route, parentRef).
		WithFilter(filter).
		MustBuild()

	assert.Equal(t, "test-plugin", plugin.Name)
	assert.Equal(t, "test-ns", plugin.Namespace)
	assert.Equal(t, "request-transformer", plugin.PluginName)
	assert.NotNil(t, plugin.Labels)
	assert.NotNil(t, plugin.Annotations)
	assert.NotEmpty(t, plugin.Config.Raw)
}
