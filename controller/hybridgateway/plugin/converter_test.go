package plugin

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

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
