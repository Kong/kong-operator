package builder

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

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

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPlugin().WithLabels(route, parentRef)

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

	t.Run("route is nil", func(t *testing.T) {
		parentRef := &gwtypes.ParentReference{Name: "test-gateway"}
		builder := NewKongPlugin().WithAnnotations(nil, parentRef)
		require.NotEmpty(t, builder.errors)
		assert.Contains(t, builder.errors[0].Error(), "route cannot be nil")
	})

	t.Run("parentRef is nil", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
		}
		builder := NewKongPlugin().WithAnnotations(route, nil)
		require.NotEmpty(t, builder.errors)
		assert.Contains(t, builder.errors[0].Error(), "parentRef cannot be nil")
	})
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
		expectedConfig transformerData
		expectError    bool
	}{
		{
			name: "add headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
						{Name: "X-Another-Header", Value: "another-value"},
					},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: transformerData{
				Append: transformerTargetSlice{
					Headers: []string{"X-Custom-Header:custom-value", "X-Another-Header:another-value"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"X-Custom-Header:custom-value", "X-Another-Header:another-value"},
					},
				},
			},
		},
		{
			name: "remove headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Remove: []string{"X-Remove-Header", "custom-value"},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{},
				},
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove-Header", "custom-value"},
				},
			},
		},
		{
			name: "set headers (add + replace)",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "Authorization", Value: "Bearer token123"},
					},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"Authorization:Bearer token123"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"Authorization:Bearer token123"},
					},
				},
			},
		},
		{
			name: "mixed operations",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Add-Header", Value: "add-value"},
					},
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Set-Header", Value: "set-value"},
					},
					Remove: []string{"X-Remove-Header"},
				},
			},
			expectedPlugin: "request-transformer",
			expectedConfig: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"X-Set-Header:set-value"},
				},
				Append: transformerTargetSlice{
					Headers: []string{"X-Add-Header:add-value"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"X-Set-Header:set-value"},
					},
				},
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove-Header"},
				},
			},
		},
		{
			name: "nil RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                  gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: nil,
			},
			expectError: true,
		},
		{
			name: "empty RequestHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                  gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{},
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

			var actualConfig transformerData
			err = json.Unmarshal(plugin.Config.Raw, &actualConfig)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.expectedConfig.Add.Headers, actualConfig.Add.Headers)
			assert.ElementsMatch(t, tt.expectedConfig.Remove.Headers, actualConfig.Remove.Headers)
		})
	}
}

func TestKongPluginBuilder_WithFilter_ResponseHeaderModifier(t *testing.T) {
	tests := []struct {
		name           string
		filter         gwtypes.HTTPRouteFilter
		expectedPlugin string
		expectedConfig transformerData
		expectError    bool
	}{
		{
			name: "add headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
						{Name: "X-Another-Header", Value: "another-value"},
					},
				},
			},
			expectedPlugin: "response-transformer",
			expectedConfig: transformerData{
				Append: transformerTargetSlice{
					Headers: []string{"X-Custom-Header:custom-value", "X-Another-Header:another-value"},
				},
			},
		},
		{
			name: "remove headers only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Remove: []string{"X-Remove-Header", "custom-value"},
				},
			},
			expectedPlugin: "response-transformer",
			expectedConfig: transformerData{
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove-Header", "custom-value"},
				},
			},
		},
		{
			name: "set headers (replace + add)",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "Authorization", Value: "Bearer token123"},
					},
				},
			},
			expectedPlugin: "response-transformer",
			expectedConfig: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"Authorization:Bearer token123"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"Authorization:Bearer token123"},
					},
				},
			},
		},
		{
			name: "mixed operations",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Add-Header", Value: "add-value"},
					},
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Set-Header", Value: "set-value"},
					},
					Remove: []string{"X-Remove-Header"},
				},
			},
			expectedPlugin: "response-transformer",
			expectedConfig: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"X-Set-Header:set-value"},
				},
				Append: transformerTargetSlice{
					Headers: []string{"X-Add-Header:add-value"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"X-Set-Header:set-value"},
					},
				},
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove-Header"},
				},
			},
		},
		{
			name: "nil ResponseHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                   gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: nil,
			},
			expectError: true,
		},
		{
			name: "empty ResponseHeaderModifier",
			filter: gwtypes.HTTPRouteFilter{
				Type:                   gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{},
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

			var actualConfig transformerData
			err = json.Unmarshal(plugin.Config.Raw, &actualConfig)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.expectedConfig.Add.Headers, actualConfig.Add.Headers)
			assert.ElementsMatch(t, tt.expectedConfig.Remove.Headers, actualConfig.Remove.Headers)
		})
	}
}

func TestKongPluginBuilder_WithFilter_UnsupportedType(t *testing.T) {
	filter := gwtypes.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterCORS, // Unsupported type
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
		expected    transformerData
		expectError bool
	}{
		{
			name: "successful translation with all operations",
			filter: gwtypes.HTTPRouteFilter{
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Add", Value: "add-val"},
					},
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Set", Value: "set-val"},
					},
					Remove: []string{"X-Remove"},
				},
			},
			expected: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"X-Set:set-val"},
				},
				Append: transformerTargetSlice{
					Headers: []string{"X-Add:add-val"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"X-Set:set-val"},
					},
				},
				Remove: transformerTargetSlice{
					Headers: []string{"X-Remove"},
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
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateRequestModifier(tt.filter)

			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, transformerData{}, result)
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
		Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
			Add: []gatewayv1.HTTPHeader{
				{Name: "X-Test", Value: "test-value"},
			},
		},
	}

	plugin := NewKongPlugin().
		WithName("test-plugin").
		WithNamespace("test-ns").
		WithLabels(route, parentRef).
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

func TestTranslateRequestRedirect(t *testing.T) {
	tests := []struct {
		name          string
		filter        gwtypes.HTTPRouteFilter
		expected      requestRedirectConfig
		expectedError string
	}{
		{
			name: "missing RequestRedirect config",
			filter: gwtypes.HTTPRouteFilter{
				Type:            gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: nil,
			},
			expectedError: "RequestRedirect filter config is missing",
		},
		{
			name: "basic redirect with default status code",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       302,
				Location:         "http://example.com/",
				KeepIncomingPath: true,
			},
		},
		{
			name: "redirect with custom status code",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					StatusCode: lo.ToPtr(301),
					Hostname:   lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       301,
				Location:         "http://example.com/",
				KeepIncomingPath: true,
			},
		},
		{
			name: "redirect with custom scheme",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Scheme:   lo.ToPtr("https"),
					Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       302,
				Location:         "https://example.com/",
				KeepIncomingPath: true,
			},
		},
		{
			name: "redirect with port",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
					Port:     lo.ToPtr(gatewayv1.PortNumber(8080)),
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       302,
				Location:         "http://example.com:8080/",
				KeepIncomingPath: true,
			},
		},
		{
			name: "redirect with full path replacement",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: lo.ToPtr("/new-path"),
					},
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       302,
				Location:         "http://example.com/new-path",
				KeepIncomingPath: false,
			},
		},
		{
			name: "redirect without hostname (path only)",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: lo.ToPtr("/redirect-path"),
					},
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       302,
				Location:         "/redirect-path",
				KeepIncomingPath: false,
			},
		},
		{
			name: "complete redirect configuration",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					StatusCode: lo.ToPtr(307),
					Scheme:     lo.ToPtr("https"),
					Hostname:   lo.ToPtr(gatewayv1.PreciseHostname("secure.example.com")),
					Port:       lo.ToPtr(gatewayv1.PortNumber(443)),
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: lo.ToPtr("/secure-path"),
					},
				},
			},
			expected: requestRedirectConfig{
				StatusCode:       307,
				Location:         "https://secure.example.com:443/secure-path",
				KeepIncomingPath: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateRequestRedirect(tt.filter)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTranslateRequestRedirectHostname(t *testing.T) {
	tests := []struct {
		name     string
		redirect *gatewayv1.HTTPRequestRedirectFilter
		expected string
	}{
		{
			name:     "nil hostname",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{},
			expected: "",
		},
		{
			name: "empty hostname",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: lo.ToPtr(gatewayv1.PreciseHostname("")),
			},
			expected: "",
		},
		{
			name: "hostname only with default scheme",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
			},
			expected: "http://example.com",
		},
		{
			name: "hostname with custom scheme",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme:   lo.ToPtr("https"),
				Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
			},
			expected: "https://example.com",
		},
		{
			name: "hostname with port",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: lo.ToPtr(gatewayv1.PreciseHostname("example.com")),
				Port:     lo.ToPtr(gatewayv1.PortNumber(8080)),
			},
			expected: "http://example.com:8080",
		},
		{
			name: "complete hostname configuration",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme:   lo.ToPtr("https"),
				Hostname: lo.ToPtr(gatewayv1.PreciseHostname("api.example.com")),
				Port:     lo.ToPtr(gatewayv1.PortNumber(443)),
			},
			expected: "https://api.example.com:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translateRequestRedirectHostname(tt.redirect)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTranslateRequestRedirectPath(t *testing.T) {
	tests := []struct {
		name          string
		redirect      *gatewayv1.HTTPRequestRedirectFilter
		expected      string
		expectedError string
	}{
		{
			name:     "nil path",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{},
			expected: "",
		},
		{
			name: "full path replacement",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: lo.ToPtr("/new-path"),
				},
			},
			expected: "/new-path",
		},
		{
			name: "full path replacement with empty string",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: lo.ToPtr(""),
				},
			},
			expected: "/",
		},
		{
			name: "full path replacement with nil value",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: nil,
				},
			},
			expected: "/",
		},
		{
			name: "prefix match replacement",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:               gatewayv1.PrefixMatchHTTPPathModifier,
					ReplacePrefixMatch: lo.ToPtr("/api"),
				},
			},
			expected: "/",
		},
		{
			name: "unsupported path modifier type",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type: "UnsupportedType",
				},
			},
			expectedError: "unsupported RequestRedirect path modifier type: UnsupportedType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateRequestRedirectPath(tt.redirect)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
