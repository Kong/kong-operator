package namegen

import (
	"fmt"
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
		name           string
		elements       []string
		expected       string
		expectedPrefix string
	}{
		{
			name:     "short name - no hashing needed",
			elements: []string{"test-route", "cp123456", "match123456"},
			expected: "test-route.cp123456.match123456",
		},
		{
			name:     "single element",
			elements: []string{"test"},
			expected: "test",
		},
		{
			name: "very long name - should hash",
			elements: []string{
				"very-long-element-that-exceeds-limits",
				"very-long-second-elemental-that-also-exceeds-normal-limits",
				"very-long-controlplane-hash-that-makes-everything-too-long",
				"and-even-more-content-to-ensure-we-exceed-the-max-length-limit-of-253-characters-for-kubernetes-resource-names-which-is-quite-a-lot-but-we-need-to-test-the-hashing-behavior-properly",
			},
			expectedPrefix: namegenPrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newName(tt.elements...)

			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			} else {
				// For very long names, verify it's hashed
				joined := strings.Join(tt.elements, ".")
				if len(joined) > maxLen {
					assert.True(t, strings.HasPrefix(result, namegenPrefix), "should start with prefix when hashed")
					assert.LessOrEqual(t, len(result), maxLen, "result should not exceed max length")
					assert.True(t, strings.HasPrefix(result, tt.expectedPrefix), "result should have the expected prefix")
				}
			}
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
			assert.GreaterOrEqual(t, len(parts), 3, fmt.Sprintf("should have at least 3 parts: %q, cp hash, and backend refs hash", httpProcolPrefix))
			assert.Equal(t, httpProcolPrefix, parts[0], fmt.Sprintf("first part should be %q", httpProcolPrefix))
			assert.True(t, strings.HasPrefix(parts[1], defaultCPPrefix), fmt.Sprintf("second part should start with %q", defaultCPPrefix))
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
			// Result should be: http.<namespace>-<name>.cp<hash>.<matches_hash>
			assert.Contains(t, result, tt.route.Namespace)
			assert.Contains(t, result, tt.route.Name)
			// Result should have multiple parts (namespace.name.cp<hash>.<matches_hash>)
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 4, fmt.Sprintf("should have at least 4 parts: %q, namespace-name, cp hash, and matches hash", httpProcolPrefix))
			assert.Equal(t, httpProcolPrefix, parts[0], fmt.Sprintf("first part should be %q", httpProcolPrefix))
			assert.Equal(t, tt.route.Namespace+"-"+tt.route.Name, parts[1], "second part should be route <namespace>-<name>")
			assert.True(t, strings.HasPrefix(parts[2], defaultCPPrefix), fmt.Sprintf("third part should start with %q", defaultCPPrefix))
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

func TestNewKongCertificateName_Generated(t *testing.T) {
	tests := []struct {
		name         string
		gatewayName  string
		listenerPort string
		expected     string
	}{
		{
			name:         "short gateway and port",
			gatewayName:  "my-gateway",
			listenerPort: "443",
			expected:     "cert.my-gateway.443",
		},
		{
			name:         "gateway with namespace prefix",
			gatewayName:  "prod-api-gateway",
			listenerPort: "8443",
			expected:     "cert.prod-api-gateway.8443",
		},
		{
			name:         "different port",
			gatewayName:  "test-gw",
			listenerPort: "80",
			expected:     "cert.test-gw.80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongCertificateName(tt.gatewayName, tt.listenerPort)
			require.NotEmpty(t, result)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Run("very long gateway name should hash", func(t *testing.T) {
		// Create a gateway name that when combined with "cert" and port exceeds 253 chars
		// "cert." (5) + longName + "." (1) + "443" (3) = need longName > 244 chars
		longGatewayName := strings.Repeat("very-long-gateway-name-segment-", 8) + "final-segment-to-exceed-limit"
		result := NewKongCertificateName(longGatewayName, "443")
		require.NotEmpty(t, result)
		require.True(t, strings.HasPrefix(result, namegenPrefix), "should hash when exceeding max length")
		require.LessOrEqual(t, len(result), maxLen)
	})
}
