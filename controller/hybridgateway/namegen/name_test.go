package namegen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestNewName(t *testing.T) {
	tests := []struct {
		name        string
		httpRouteID string
		parentRefID string
		sectionID   string
		expected    *Name
	}{
		{
			name:        "all parameters provided",
			httpRouteID: "test-ns-test-route",
			parentRefID: "cp123456",
			sectionID:   "res789",
			expected: &Name{
				httpRouteID:    "test-ns-test-route",
				controlPlaneID: "cp123456",
				sectionID:      "res789",
			},
		},
		{
			name:        "empty section ID",
			httpRouteID: "test-ns-test-route",
			parentRefID: "cp123456",
			sectionID:   "",
			expected: &Name{
				httpRouteID:    "test-ns-test-route",
				controlPlaneID: "cp123456",
				sectionID:      "",
			},
		},
		{
			name:        "all empty strings",
			httpRouteID: "",
			parentRefID: "",
			sectionID:   "",
			expected: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newName(tt.httpRouteID, tt.parentRefID, tt.sectionID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_String(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     *Name
		expected    string
		description string
	}{
		{
			name: "short name with section",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "cp123",
				sectionID:      "res456",
			},
			expected:    "test-ns-route.cp123.res456",
			description: "should join all parts with dots when length is acceptable",
		},
		{
			name: "short name without http route",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "cp123",
				sectionID:      "res456",
			},
			expected:    "cp123.res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name without parent reference",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "",
				sectionID:      "res456",
			},
			expected:    "test-ns-route.res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name without section",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "cp123",
				sectionID:      "",
			},
			expected:    "test-ns-route.cp123",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just httproute",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "",
				sectionID:      "",
			},
			expected:    "test-ns-route",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just parentref",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "cp123",
				sectionID:      "",
			},
			expected:    "cp123",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just section",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "res456",
			},
			expected:    "res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "empty name",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "",
			},
			expected:    "",
			description: "should handle empty strings gracefully",
		},
		{
			name: "very long names that need hashing",
			nameObj: &Name{
				httpRouteID:    strings.Repeat("a", 100),
				controlPlaneID: strings.Repeat("b", 100),
				sectionID:      strings.Repeat("c", 100),
			},
			expected:    "", // Will be set dynamically in test
			description: "should hash long components and stay within length limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.String()

			if tt.name == "very long names that need hashing" {
				// For long names, verify the result is within limits and has expected prefixes
				assert.LessOrEqual(t, len(result), 253-len("faf385ae")-1, "result should be within max length")
				parts := strings.Split(result, ".")
				require.Len(t, parts, 3, "should have 3 parts after hashing")
				assert.True(t, strings.HasPrefix(parts[0], "http"), "first part should have 'http' prefix")
				assert.True(t, strings.HasPrefix(parts[1], "cp"), "second part should have 'cp' prefix")
				assert.True(t, strings.HasPrefix(parts[2], "res"), "third part should have 'res' prefix")
			} else {
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestName_String_Hashing(t *testing.T) {
	// Create names that will definitely trigger hashing
	longHTTPRoute := strings.Repeat("httproute", 50)
	longParentRef := strings.Repeat("parentref", 50)
	longSection := strings.Repeat("section", 50)

	nameObj := newName(longHTTPRoute, longParentRef, longSection)
	result := nameObj.String()

	// Should be hashed and within limits
	maxLen := 253 - len("faf385ae") - 1
	assert.LessOrEqual(t, len(result), maxLen)

	// Should contain the expected prefixes for hashed components
	parts := strings.Split(result, ".")
	assert.Len(t, parts, 3)
	assert.True(t, strings.HasPrefix(parts[0], "http"))
	assert.True(t, strings.HasPrefix(parts[1], "cp"))
	assert.True(t, strings.HasPrefix(parts[2], "res"))

	// Each hashed part should be reasonably short
	for _, part := range parts {
		assert.LessOrEqual(t, len(part), 50, "hashed parts should be reasonably short")
	}
}

func TestName_String_Consistency(t *testing.T) {
	tests := []struct {
		name        string
		httpRouteID string
		parentRefID string
		sectionID   string
	}{
		{
			name:        "normal components",
			httpRouteID: "test-route",
			parentRefID: "cp123",
			sectionID:   "res456",
		},
		{
			name:        "long components (trigger hashing)",
			httpRouteID: strings.Repeat("httproute", 50),
			parentRefID: strings.Repeat("parentref", 50),
			sectionID:   strings.Repeat("section", 50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameObj1 := newName(tt.httpRouteID, tt.parentRefID, tt.sectionID)
			nameObj2 := newName(tt.httpRouteID, tt.parentRefID, tt.sectionID)

			result1 := nameObj1.String()
			result2 := nameObj2.String()

			assert.Equal(t, result1, result2, "same inputs should produce same outputs")
		})
	}
}

func TestNewKongUpstreamName(t *testing.T) {
	tests := []struct {
		name string
		cp   *commonv1alpha1.ControlPlaneRef
		rule gatewayv1.HTTPRouteRule
	}{
		{
			name: "basic upstream name generation",
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: "service1",
								Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
							},
						},
					},
				},
			},
		},
		{
			name: "multiple backend refs",
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "multi-cp",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: "service1",
								Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
							},
						},
					},
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: "service2",
								Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(9090); return &p }(),
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongUpstreamName(tt.cp, tt.rule)
			assert.NotEmpty(t, result)
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 2)
			assert.True(t, strings.HasPrefix(parts[0], "cp"))
		})
	}
}

func TestNewKongServiceName(t *testing.T) {
	tests := []struct {
		name string
		cp   *commonv1alpha1.ControlPlaneRef
		rule gatewayv1.HTTPRouteRule
	}{
		{
			name: "basic service name generation",
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: "service1",
								Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
							},
						},
					},
				},
			},
		},
		{
			name: "service with namespace",
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name:      "namespaced-cp",
					Namespace: "konnect-system",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name:      "backend-service",
								Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("backend-ns"); return &ns }(),
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongServiceName(tt.cp, tt.rule)
			assert.NotEmpty(t, result)
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 2)
			assert.True(t, strings.HasPrefix(parts[0], "cp"))
		})
	}
}

func TestNewKongRouteName(t *testing.T) {
	tests := []struct {
		name  string
		route *gwtypes.HTTPRoute
		cp    *commonv1alpha1.ControlPlaneRef
		rule  gatewayv1.HTTPRouteRule
	}{
		{
			name: "basic route name generation",
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  func() *gatewayv1.PathMatchType { t := gatewayv1.PathMatchPathPrefix; return &t }(),
							Value: func() *string { s := "/api"; return &s }(),
						},
					},
				},
			},
		},
		{
			name: "route with multiple matches",
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-route",
					Namespace: "test-ns",
				},
			},
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "complex-cp",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  func() *gatewayv1.PathMatchType { t := gatewayv1.PathMatchPathPrefix; return &t }(),
							Value: func() *string { s := "/api/v1"; return &s }(),
						},
						Headers: []gatewayv1.HTTPHeaderMatch{
							{
								Name:  "X-API-Version",
								Value: "v1",
							},
						},
					},
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  func() *gatewayv1.PathMatchType { t := gatewayv1.PathMatchExact; return &t }(),
							Value: func() *string { s := "/health"; return &s }(),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongRouteName(tt.route, tt.cp, tt.rule)
			assert.NotEmpty(t, result)
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 2)
			assert.Equal(t, tt.route.Namespace+"-"+tt.route.Name, parts[0])
		})
	}
}

func TestNewKongPluginName(t *testing.T) {
	tests := []struct {
		name   string
		filter gatewayv1.HTTPRouteFilter
	}{
		{
			name: "request header modifier filter",
			filter: gatewayv1.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Test", Value: "test-value"},
					},
				},
			},
		},
		{
			name: "response header modifier filter",
			filter: gatewayv1.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterResponseHeaderModifier,
				ResponseHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Add: []gatewayv1.HTTPHeader{
						{Name: "X-Response", Value: "response-value"},
					},
				},
			},
		},
		{
			name: "request redirect filter",
			filter: gatewayv1.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					StatusCode: func() *int { s := 301; return &s }(),
					Hostname:   func() *gatewayv1.PreciseHostname { h := gatewayv1.PreciseHostname("example.com"); return &h }(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongPluginName(tt.filter)
			assert.NotEmpty(t, result)
			assert.True(t, strings.HasPrefix(result, "pl"))
		})
	}
}

func TestNewKongPluginBindingName(t *testing.T) {
	tests := []struct {
		name     string
		routeID  string
		pluginId string
		expected string
	}{
		{
			name:     "basic plugin binding name",
			routeID:  "default-test-route.cp12345678.ab123456",
			pluginId: "pl87654321",
			expected: "default-test-route.cp12345678.ab123456..pl87654321",
		},
		{
			name:     "empty route ID",
			routeID:  "",
			pluginId: "pl99887766",
			expected: "pl99887766",
		},
		{
			name:     "long route ID",
			routeID:  "very-long-namespace-name-very-long-route-name.cp12345678.ab123456",
			pluginId: "pl11223344",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongPluginBindingName(tt.routeID, tt.pluginId)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, tt.pluginId)
			if tt.routeID != "" {
				assert.Contains(t, result, tt.routeID)
			}
		})
	}
}

func TestNewKongTargetName(t *testing.T) {
	tests := []struct {
		name       string
		upstreamID string
		endpointID string
		port       int
		br         *gwtypes.HTTPBackendRef
	}{
		{
			name:       "basic target name",
			upstreamID: "cp12345678.ab123456",
			endpointID: "192.168.1.100",
			port:       8080,
			br: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "backend-service",
						Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
					},
				},
			},
		},
		{
			name:       "target with different port",
			upstreamID: "cp87654321.cd987654",
			endpointID: "10.0.0.50",
			port:       9090,
			br: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name:      "api-service",
						Namespace: func() *gatewayv1.Namespace { ns := gatewayv1.Namespace("api-ns"); return &ns }(),
						Port:      func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(9090); return &p }(),
					},
					Weight: func() *int32 { w := int32(100); return &w }(),
				},
			},
		},
		{
			name:       "target with IPv6 endpoint",
			upstreamID: "cp11223344.ef556677",
			endpointID: "2001:db8::1",
			port:       443,
			br: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "secure-service",
						Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(443); return &p }(),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongTargetName(tt.upstreamID, tt.endpointID, tt.port, tt.br)
			assert.NotEmpty(t, result)
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 2)
			assert.Equal(t, tt.upstreamID, strings.Join(parts[0:2], "."))
		})
	}
}

func TestNameGenerationConsistency(t *testing.T) {
	// Test that the same inputs always produce the same outputs
	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "consistent-cp",
		},
	}

	rule := gatewayv1.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "consistent-service",
						Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
					},
				},
			},
		},
	}

	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consistent-route",
			Namespace: "default",
		},
	}

	filter := gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
			Set: []gatewayv1.HTTPHeader{
				{Name: "X-Consistent", Value: "test"},
			},
		},
	}

	br := &gwtypes.HTTPBackendRef{
		BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Name: "consistent-backend",
				Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
			},
		},
	}

	t.Run("upstream name consistency", func(t *testing.T) {
		result1 := NewKongUpstreamName(cp, rule)
		result2 := NewKongUpstreamName(cp, rule)
		assert.Equal(t, result1, result2)
	})

	t.Run("service name consistency", func(t *testing.T) {
		result1 := NewKongServiceName(cp, rule)
		result2 := NewKongServiceName(cp, rule)
		assert.Equal(t, result1, result2)
	})

	t.Run("route name consistency", func(t *testing.T) {
		ruleWithMatches := gatewayv1.HTTPRouteRule{
			Matches: []gatewayv1.HTTPRouteMatch{
				{
					Path: &gatewayv1.HTTPPathMatch{
						Type:  func() *gatewayv1.PathMatchType { t := gatewayv1.PathMatchPathPrefix; return &t }(),
						Value: func() *string { s := "/api"; return &s }(),
					},
				},
			},
		}
		result1 := NewKongRouteName(route, cp, ruleWithMatches)
		result2 := NewKongRouteName(route, cp, ruleWithMatches)
		assert.Equal(t, result1, result2)
	})

	t.Run("plugin name consistency", func(t *testing.T) {
		result1 := NewKongPluginName(filter)
		result2 := NewKongPluginName(filter)
		assert.Equal(t, result1, result2)
	})

	t.Run("target name consistency", func(t *testing.T) {
		upstreamID := "test-upstream"
		endpointID := "192.168.1.1"
		port := 8080
		result1 := NewKongTargetName(upstreamID, endpointID, port, br)
		result2 := NewKongTargetName(upstreamID, endpointID, port, br)
		assert.Equal(t, result1, result2)
	})
}
