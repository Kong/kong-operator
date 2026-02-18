package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
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
					StatusCode: new(301),
					Hostname:   new(gatewayv1.PreciseHostname("example.com")),
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
					Scheme:   new("https"),
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
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
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
					Port:     new(gatewayv1.PortNumber(8080)),
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
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: new("/new-path"),
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
						ReplaceFullPath: new("/redirect-path"),
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
					StatusCode: new(307),
					Scheme:     new("https"),
					Hostname:   new(gatewayv1.PreciseHostname("secure.example.com")),
					Port:       new(gatewayv1.PortNumber(443)),
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: new("/secure-path"),
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
				Hostname: new(gatewayv1.PreciseHostname("")),
			},
			expected: "",
		},
		{
			name: "hostname only with default scheme",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: new(gatewayv1.PreciseHostname("example.com")),
			},
			expected: "http://example.com",
		},
		{
			name: "hostname with custom scheme",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme:   new("https"),
				Hostname: new(gatewayv1.PreciseHostname("example.com")),
			},
			expected: "https://example.com",
		},
		{
			name: "hostname with port",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Hostname: new(gatewayv1.PreciseHostname("example.com")),
				Port:     new(gatewayv1.PortNumber(8080)),
			},
			expected: "http://example.com:8080",
		},
		{
			name: "complete hostname configuration",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Scheme:   new("https"),
				Hostname: new(gatewayv1.PreciseHostname("api.example.com")),
				Port:     new(gatewayv1.PortNumber(443)),
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

func TestTranslateRequestRedirectPreFunction(t *testing.T) {
	tests := []struct {
		name          string
		filter        gwtypes.HTTPRouteFilter
		rule          gwtypes.HTTPRouteRule
		expectedError string
		validateFunc  func(t *testing.T, config accessPreFunctionConfig)
	}{
		{
			name: "missing RequestRedirect config",
			filter: gwtypes.HTTPRouteFilter{
				Type:            gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: nil,
			},
			rule:          gwtypes.HTTPRouteRule{},
			expectedError: "RequestRedirect filter config is missing",
		},
		{
			name: "missing RequestRedirect ReplacePrefixMatch modifier",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: nil,
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/api"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				// When ReplacePrefixMatch is nil, it should use empty string
				assert.Contains(t, luaCode, `local custom_prefix = [[]]`)
				assert.Contains(t, luaCode, `local match_prefix = [[/api]]`)
			},
		},
		{
			name: "basic prefix match redirect with defaults",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/new-api"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/api"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local match_prefix = [[/api]]`)
				assert.Contains(t, luaCode, `local custom_prefix = [[/new-api]]`)
				assert.Contains(t, luaCode, `local custom_host = [[]]`)
				assert.Contains(t, luaCode, `local custom_scheme = [[]]`)
				assert.Contains(t, luaCode, `local code = 302`)
				assert.Contains(t, luaCode, `kong.response.exit(code`)
			},
		},
		{
			name: "prefix match with custom scheme",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Scheme: new("https"),
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/secure"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/old"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local custom_scheme = [[https]]`)
			},
		},
		{
			name: "prefix match with custom host and port",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: new(gatewayv1.PreciseHostname("new.example.com")),
					Port:     new(gatewayv1.PortNumber(8443)),
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/v2"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/v1"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local custom_host = [[new.example.com]]`)
			},
		},
		{
			name: "prefix match with custom status code",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					StatusCode: new(301),
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/permanent"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/temp"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local code = 301`)
			},
		},
		{
			name: "prefix match with all options",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Scheme:     new("https"),
					Hostname:   new(gatewayv1.PreciseHostname("secure.example.com")),
					Port:       new(gatewayv1.PortNumber(443)),
					StatusCode: new(308),
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/api/v2"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/api/v1"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local match_prefix = [[/api/v1]]`)
				assert.Contains(t, luaCode, `local custom_prefix = [[/api/v2]]`)
				assert.Contains(t, luaCode, `local custom_host = [[secure.example.com]]`)
				assert.Contains(t, luaCode, `local custom_scheme = [[https]]`)
				assert.Contains(t, luaCode, `local code = 308`)
			},
		},
		{
			name: "prefix match with special characters in path",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/new-path/with-dashes_and_underscores"),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/old-path"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `[[/new-path/with-dashes_and_underscores]]`)
			},
		},
		{
			name: "prefix match with empty replacement prefix",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new(""),
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/remove-prefix"),
						},
					},
				},
			},
			validateFunc: func(t *testing.T, config accessPreFunctionConfig) {
				require.Len(t, config.Access, 1)
				luaCode := config.Access[0]
				assert.Contains(t, luaCode, `local custom_prefix = [[]]`)
				assert.Contains(t, luaCode, `local match_prefix = [[/remove-prefix]]`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateRequestRedirectPreFunction(tt.filter, tt.rule)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				if tt.validateFunc != nil {
					tt.validateFunc(t, result)
				}
			}
		})
	}
}

func TestTranslateRequestRedirectGenerateFunctionBody(t *testing.T) {
	tests := []struct {
		name         string
		sourcePrefix string
		targetPrefix string
		targetHost   string
		customScheme string
		customCode   int
		validate     func(t *testing.T, luaCode string)
	}{
		{
			name:         "all custom parameters",
			sourcePrefix: "/api/v1",
			targetPrefix: "/api/v2",
			targetHost:   "https://api.example.com:443",
			customScheme: "https",
			customCode:   307,
			validate: func(t *testing.T, luaCode string) {
				assert.Contains(t, luaCode, `local match_prefix = [[/api/v1]]`)
				assert.Contains(t, luaCode, `local custom_prefix = [[/api/v2]]`)
				assert.Contains(t, luaCode, `local custom_host = [[https://api.example.com:443]]`)
				assert.Contains(t, luaCode, `local custom_scheme = [[https]]`)
				assert.Contains(t, luaCode, `local code = 307`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translateRequestRedirectGenerateFunctionBody(
				tt.sourcePrefix,
				tt.targetPrefix,
				tt.targetHost,
				tt.customScheme,
				tt.customCode,
			)

			assert.NotEmpty(t, result)
			if tt.validate != nil {
				tt.validate(t, result)
			}

			// Verify it's valid Lua structure (basic syntax check)
			assert.Contains(t, result, "-- Inputs")
			assert.Contains(t, result, "local match_prefix")
			assert.Contains(t, result, "local custom_prefix")
			assert.Contains(t, result, "local custom_host")
			assert.Contains(t, result, "local custom_scheme")
			assert.Contains(t, result, "local code")
		})
	}
}

func TestTranslateFromFilterWithPrefixMatchRedirect(t *testing.T) {
	tests := []struct {
		name          string
		rule          gwtypes.HTTPRouteRule
		filter        gwtypes.HTTPRouteFilter
		expectedError string
		validate      func(t *testing.T, configs []kongPluginConfig)
	}{
		{
			name: "RequestRedirect with PrefixMatch uses pre-function plugin",
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/api/v1"),
						},
					},
				},
			},
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/api/v2"),
					},
				},
			},
			validate: func(t *testing.T, configs []kongPluginConfig) {
				require.Len(t, configs, 1)
				assert.Equal(t, "pre-function", configs[0].name)
				assert.NotEmpty(t, configs[0].config)

				// Verify it's valid JSON for accessPreFunctionConfig
				var preFuncConfig accessPreFunctionConfig
				err := json.Unmarshal(configs[0].config, &preFuncConfig)
				require.NoError(t, err)
				require.Len(t, preFuncConfig.Access, 1)
				assert.Contains(t, preFuncConfig.Access[0], `local match_prefix = [[/api/v1]]`)
			},
		},
		{
			name: "RequestRedirect with FullPath uses redirect plugin",
			rule: gwtypes.HTTPRouteRule{},
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: new("/new-path"),
					},
				},
			},
			validate: func(t *testing.T, configs []kongPluginConfig) {
				require.Len(t, configs, 1)
				assert.Equal(t, "redirect", configs[0].name)
				assert.NotEmpty(t, configs[0].config)

				// Verify it's valid JSON for requestRedirectConfig
				var redirectConfig requestRedirectConfig
				err := json.Unmarshal(configs[0].config, &redirectConfig)
				require.NoError(t, err)
				assert.Equal(t, "http://example.com/new-path", redirectConfig.Location)
			},
		},
		{
			name: "RequestRedirect without path uses redirect plugin",
			rule: gwtypes.HTTPRouteRule{},
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Hostname: new(gatewayv1.PreciseHostname("example.com")),
				},
			},
			validate: func(t *testing.T, configs []kongPluginConfig) {
				require.Len(t, configs, 1)
				assert.Equal(t, "redirect", configs[0].name)
				assert.NotEmpty(t, configs[0].config)

				// Verify it's valid JSON for requestRedirectConfig
				var redirectConfig requestRedirectConfig
				err := json.Unmarshal(configs[0].config, &redirectConfig)
				require.NoError(t, err)
				assert.Equal(t, "http://example.com/", redirectConfig.Location)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateFromFilter(tt.rule, tt.filter)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
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
					ReplaceFullPath: new("/new-path"),
				},
			},
			expected: "/new-path",
		},
		{
			name: "full path replacement with empty string",
			redirect: &gatewayv1.HTTPRequestRedirectFilter{
				Path: &gatewayv1.HTTPPathModifier{
					Type:            gatewayv1.FullPathHTTPPathModifier,
					ReplaceFullPath: new(""),
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
					ReplacePrefixMatch: new("/api"),
				},
			},
			expected: "",
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

func TestTranslateURLRewrite(t *testing.T) {
	tests := []struct {
		name          string
		filter        gwtypes.HTTPRouteFilter
		path          string
		expected      transformerData
		expectedError string
	}{
		{
			name: "missing URLRewrite config",
			filter: gwtypes.HTTPRouteFilter{
				Type:       gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: nil,
			},
			path:          "/api",
			expectedError: "URLRewrite filter config is missing",
		},
		{
			name: "hostname rewrite only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Hostname: new(gatewayv1.PreciseHostname("new-host.example.com")),
				},
			},
			path: "/api",
			expected: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"host:new-host.example.com"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"host:new-host.example.com"},
					},
				},
			},
		},
		{
			name: "full path rewrite only",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: new("/new-path"),
					},
				},
			},
			path: "/api",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: "/new-path",
				},
			},
		},
		{
			name: "full path rewrite with empty string",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: new(""),
					},
				},
			},
			path: "/api",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: "/",
				},
			},
		},
		{
			name: "prefix match rewrite - root path with non-root prefix",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/api/v2"),
					},
				},
			},
			path: "/",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: `/api/v2$(uri_captures[1] == nil and "" or "/" .. uri_captures[1])`,
				},
			},
		},
		{
			name: "prefix match rewrite - non-root path with non-root prefix",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/api/v2"),
					},
				},
			},
			path: "/api/v1",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: `/api/v2$(uri_captures[1])`,
				},
			},
		},
		{
			name: "prefix match rewrite - root path with empty prefix",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new(""),
					},
				},
			},
			path: "/",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: `$(uri_captures[1] == nil and "/" or "/" .. uri_captures[1])`,
				},
			},
		},
		{
			name: "prefix match rewrite - non-root path with empty prefix",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new(""),
					},
				},
			},
			path: "/api/v1",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: `$(uri_captures[1] == nil and "/" or uri_captures[1])`,
				},
			},
		},
		{
			name: "prefix match rewrite - path with trailing slash",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/new/api/"),
					},
				},
			},
			path: "/old/api/",
			expected: transformerData{
				Replace: transformerTargetSliceReplace{
					URI: `/new/api$(uri_captures[1])`,
				},
			},
		},
		{
			name: "hostname and path rewrite combined",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Hostname: new(gatewayv1.PreciseHostname("new-host.example.com")),
					Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/api/v2"),
					},
				},
			},
			path: "/api/v1",
			expected: transformerData{
				Add: transformerTargetSlice{
					Headers: []string{"host:new-host.example.com"},
				},
				Replace: transformerTargetSliceReplace{
					transformerTargetSlice: transformerTargetSlice{
						Headers: []string{"host:new-host.example.com"},
					},
					URI: `/api/v2$(uri_captures[1])`,
				},
			},
		},
		{
			name: "unsupported path modifier type",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterURLRewrite,
				URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type: "UnsupportedType",
					},
				},
			},
			path:          "/api",
			expectedError: "unsupported URLRewrite path modifier type: UnsupportedType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translateURLRewrite(tt.filter, tt.path)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Add.Headers, result.Add.Headers)
				assert.Equal(t, tt.expected.Replace.Headers, result.Replace.Headers)
				assert.Equal(t, tt.expected.Replace.URI, result.Replace.URI)
			}
		})
	}
}
