package namegen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func testRoute(namespace, name string) *gwtypes.HTTPRoute {
	return &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func testControlPlaneRef(name string) *commonv1alpha1.ControlPlaneRef {
	return &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: name,
		},
	}
}

func testParentRef(sectionName *gatewayv1.SectionName) *gwtypes.ParentReference {
	return &gwtypes.ParentReference{
		Name:        gatewayv1.ObjectName("test-gateway"),
		Namespace:   new(gatewayv1.Namespace("default")),
		SectionName: sectionName,
	}
}

func testPathMatch(path string) []gatewayv1.HTTPRouteMatch {
	matchType := gatewayv1.PathMatchPathPrefix
	return []gatewayv1.HTTPRouteMatch{
		{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  &matchType,
				Value: &path,
			},
		},
	}
}

func testBackendRef(
	name string, namespace *gatewayv1.Namespace, port *gatewayv1.PortNumber,
) gatewayv1.HTTPBackendRef {
	return gatewayv1.HTTPBackendRef{
		BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Name:      gatewayv1.ObjectName(name),
				Namespace: namespace,
				Port:      port,
			},
		},
	}
}

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

func TestNewKongUpstreamNameForHTTPRoute(t *testing.T) {
	tests := []struct {
		name     string
		route    *gwtypes.HTTPRoute
		cp       *commonv1alpha1.ControlPlaneRef
		rule     gatewayv1.HTTPRouteRule
		expected string
	}{
		{
			name:  "basic upstream name generation",
			route: testRoute("default", "test-route"),
			cp:    testControlPlaneRef("test-cp"),
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					testBackendRef("service1", nil, new(gatewayv1.PortNumber(8080))),
				},
			},
			expected: "http.default-service1-8080.1.cp1fbfa93f.e25441d7",
		},
		{
			name:  "multiple backend refs use lowest lexical backend in readable prefix",
			route: testRoute("default", "test-route"),
			cp:    testControlPlaneRef("multi-cp"),
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					testBackendRef("service2", nil, new(gatewayv1.PortNumber(9090))),
					testBackendRef("service1", nil, new(gatewayv1.PortNumber(8080))),
				},
			},
			expected: "http.default-service1-8080.2.cp918460a.2c5d1acf",
		},
		{
			name:  "cross namespace backend is reflected in readable prefix",
			route: testRoute("default", "test-route"),
			cp:    testControlPlaneRef("namespaced-cp"),
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					testBackendRef("backend-service", new(gatewayv1.Namespace("backend-ns")), nil),
				},
			},
			expected: "http.backend-ns-backend-service.1.cpba78230e.1f2a8c5",
		},
		{
			name:  "backendless rule produces readable prefix with just http and cp hash",
			route: testRoute("default", "test-route"),
			cp:    testControlPlaneRef("namespaced-cp"),
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{},
				Filters: []gatewayv1.HTTPRouteFilter{
					{
						Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
						RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
							Set: []gatewayv1.HTTPHeader{
								{
									Name:  "header",
									Value: "value",
								},
							},
						},
					},
				},
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/prefix"),
						},
					},
				},
			},
			expected: "http.cpba78230e.7f99a851",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongUpstreamNameForHTTPRouteRule(tt.route, tt.cp, tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewKongUpstreamName_Equality(t *testing.T) {
	tests := []struct {
		name  string
		route *gwtypes.HTTPRoute
		cp    *commonv1alpha1.ControlPlaneRef
		ruleA gatewayv1.HTTPRouteRule
		ruleB gatewayv1.HTTPRouteRule
		equal bool
	}{
		{
			name:  "different matches with no backends produce different results",
			route: testRoute("gateway-conformance-infra", "redirect-host-and-status"),
			cp:    testControlPlaneRef("same-namespace-rrfhd"),
			ruleA: gatewayv1.HTTPRouteRule{Matches: testPathMatch("/hostname-redirect")},
			ruleB: gatewayv1.HTTPRouteRule{Matches: testPathMatch("/host-and-status")},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameA := NewKongUpstreamNameForHTTPRouteRule(tt.route, tt.cp, tt.ruleA)
			nameB := NewKongUpstreamNameForHTTPRouteRule(tt.route, tt.cp, tt.ruleB)

			if tt.equal {
				assert.Equal(t, nameA, nameB)
			} else {
				assert.NotEqual(t, nameA, nameB)
			}
		})
	}
}

func TestNewKongServiceName(t *testing.T) {
	tests := []struct {
		name             string
		route            *gwtypes.HTTPRoute
		cp               *commonv1alpha1.ControlPlaneRef
		rule             gatewayv1.HTTPRouteRule
		expectedReadable string
	}{
		{
			name:  "basic service name generation",
			route: testRoute("default", "test-route"),
			cp:    testControlPlaneRef("test-cp"),
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					testBackendRef("service1", nil, new(gatewayv1.PortNumber(8080))),
				},
			},
			expectedReadable: "http.default-service1-8080.1",
		},
		{
			name:  "service with namespace",
			route: testRoute("default", "test-route"),
			cp: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name:      "namespaced-cp",
					Namespace: "konnect-system",
				},
			},
			rule: gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					testBackendRef("backend-service", new(gatewayv1.Namespace("backend-ns")), nil),
				},
			},
			expectedReadable: "http.backend-ns-backend-service.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongServiceNameForHTTPRouteRule(tt.route, tt.cp, tt.rule)
			expected := tt.expectedReadable + ".cp" + utils.Hash32(tt.cp) + "." + utils.Hash32(tt.rule.BackendRefs)
			assert.Equal(t, expected, result)
		})
	}
}

func TestNewKongServiceNameForHTTPRouteRuleBackendNotFound(t *testing.T) {
	backendNS := gatewayv1.Namespace("gateway-conformance-web-backend")
	port := gatewayv1.PortNumber(8080)
	rule := gatewayv1.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			testBackendRef("web-backend", &backendNS, &port),
		},
	}
	routeA := testRoute("gateway-conformance-infra", "invalid-cross-namespace-backend-ref")
	routeB := testRoute("gateway-conformance-infra", "reference-grant")
	cp := testControlPlaneRef("same-namespace")

	normalName := NewKongServiceNameForHTTPRouteRule(routeA, cp, rule)
	fallbackNameA := NewKongServiceNameForHTTPRouteRuleBackendNotFound(routeA, cp, rule)
	fallbackNameB := NewKongServiceNameForHTTPRouteRuleBackendNotFound(routeB, cp, rule)

	assert.NotEqual(t, normalName, fallbackNameA)
	assert.NotEqual(t, fallbackNameA, fallbackNameB)
	assert.Contains(t, fallbackNameA, backendNotFoundPrefix)
}

func TestNewKongServiceName_Equality(t *testing.T) {
	tests := []struct {
		name  string
		route *gwtypes.HTTPRoute
		cp    *commonv1alpha1.ControlPlaneRef
		ruleA gatewayv1.HTTPRouteRule
		ruleB gatewayv1.HTTPRouteRule
		equal bool
	}{
		{
			name:  "different matches with no backends produce different results",
			route: testRoute("gateway-conformance-infra", "redirect-host-and-status"),
			cp:    testControlPlaneRef("same-namespace-rrfhd"),
			ruleA: gatewayv1.HTTPRouteRule{Matches: testPathMatch("/hostname-redirect")},
			ruleB: gatewayv1.HTTPRouteRule{Matches: testPathMatch("/host-and-status")},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameA := NewKongServiceNameForHTTPRouteRule(tt.route, tt.cp, tt.ruleA)
			nameB := NewKongServiceNameForHTTPRouteRule(tt.route, tt.cp, tt.ruleB)

			if tt.equal {
				assert.Equal(t, nameA, nameB)
			} else {
				assert.NotEqual(t, nameA, nameB)
			}
		})
	}
}

func TestNewKongServiceName_BackendRequestTimeout(t *testing.T) {
	backendNS := gatewayv1.Namespace("gateway-conformance-infra")
	port := gatewayv1.PortNumber(8080)
	route := testRoute("gateway-conformance-infra", "backend-request-timeout")
	cp := testControlPlaneRef("same-namespace")

	ruleWith := func(timeout *gatewayv1.Duration) gatewayv1.HTTPRouteRule {
		rule := gatewayv1.HTTPRouteRule{
			BackendRefs: []gatewayv1.HTTPBackendRef{testBackendRef("infra-backend-v1", &backendNS, &port)},
		}
		if timeout != nil {
			rule.Timeouts = &gatewayv1.HTTPRouteTimeouts{BackendRequest: timeout}
		}
		return rule
	}

	d500ms := gatewayv1.Duration("500ms")
	d0s := gatewayv1.Duration("0s")
	dHalfS := gatewayv1.Duration("0.5s")

	noTimeout := NewKongServiceNameForHTTPRouteRule(route, cp, ruleWith(nil))
	timeout500ms := NewKongServiceNameForHTTPRouteRule(route, cp, ruleWith(&d500ms))
	timeout0s := NewKongServiceNameForHTTPRouteRule(route, cp, ruleWith(&d0s))
	timeoutHalfS := NewKongServiceNameForHTTPRouteRule(route, cp, ruleWith(&dHalfS))

	// Same backends but different timeouts must produce distinct KongServices (the conformance
	// case: /backend-timeout=500ms and /disable-backend-timeout=0s share infra-backend-v1).
	assert.NotEqual(t, timeout500ms, timeout0s)
	// A rule with a timeout differs from the same rule without one.
	assert.NotEqual(t, noTimeout, timeout500ms)
	assert.NotEqual(t, noTimeout, timeout0s)
	// Backward compatibility: a rule without a backendRequest timeout keeps the original name.
	assert.Equal(t, noTimeout, NewKongServiceNameForHTTPRouteRule(route, cp, ruleWith(nil)))
	// Equivalent durations normalize to the same name (no spurious split).
	assert.Equal(t, timeout500ms, timeoutHalfS)
}

func TestNewKongServiceName_BackendDisplayLimit(t *testing.T) {
	port := func(value gatewayv1.PortNumber) *gatewayv1.PortNumber { return &value }
	backendRef := func(name string, namespace *gatewayv1.Namespace, portNumber *gatewayv1.PortNumber) gatewayv1.HTTPBackendRef {
		return gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      gatewayv1.ObjectName(name),
					Namespace: namespace,
					Port:      portNumber,
				},
			},
		}
	}
	buildExpected := func(readable string, cp *commonv1alpha1.ControlPlaneRef, backends []gatewayv1.HTTPBackendRef) string {
		hashPart := "cp" + utils.Hash32(cp) + "." + utils.Hash32(backends)
		name := readable + "." + hashPart
		if len(name) <= maxLen {
			return name
		}
		remaining := maxLen - len(hashPart) - 1
		if remaining <= 0 {
			return hashPart
		}
		readable = strings.TrimRight(readable[:remaining], ".-")
		if readable == "" {
			return hashPart
		}
		return readable + "." + hashPart
	}

	tests := []struct {
		name     string
		route    *gwtypes.HTTPRoute
		cp       *commonv1alpha1.ControlPlaneRef
		backends []gatewayv1.HTTPBackendRef
		readable string
	}{
		{
			name: "short backend names",
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
			backends: []gatewayv1.HTTPBackendRef{
				backendRef("svc-b", nil, port(8080)),
				backendRef("svc-a", nil, port(8080)),
				backendRef("svc-c", nil, port(8080)),
			},
			readable: "http.default-svc-a-8080.3",
		},
		{
			name: "three long service names",
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
			backends: []gatewayv1.HTTPBackendRef{
				backendRef(strings.Repeat("a", 63), nil, port(8080)),
				backendRef(strings.Repeat("b", 63), nil, port(8080)),
				backendRef(strings.Repeat("c", 63), nil, port(8080)),
			},
			readable: "http.default-" + strings.Repeat("a", 63) + "-8080.3",
		},
		{
			name: "two long namespaces",
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
			backends: []gatewayv1.HTTPBackendRef{
				backendRef("service-a", func() *gatewayv1.Namespace { ns := gatewayv1.Namespace(strings.Repeat("n", 220)); return &ns }(), port(8080)),
				backendRef("service-b", func() *gatewayv1.Namespace { ns := gatewayv1.Namespace(strings.Repeat("n", 220)); return &ns }(), port(8080)),
			},
			readable: "http." + strings.Repeat("n", 220) + "-service-a-8080.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := gatewayv1.HTTPRouteRule{BackendRefs: tt.backends}
			result := NewKongServiceNameForHTTPRouteRule(tt.route, tt.cp, rule)
			expected := buildExpected(tt.readable, tt.cp, tt.backends)
			assert.Equal(t, expected, result)
		})
	}
}

func TestNewKongRouteNameForMatch_DiffersByParentRef(t *testing.T) {
	route := testRoute("default", "test-route")
	cp := testControlPlaneRef("test-cp")
	matchType := gatewayv1.PathMatchPathPrefix
	match := gatewayv1.HTTPRouteMatch{
		Path: &gatewayv1.HTTPPathMatch{
			Type:  &matchType,
			Value: new("/"),
		},
	}
	listener1 := gatewayv1.SectionName("listener-1")
	listener2 := gatewayv1.SectionName("listener-2")

	name1 := NewKongRouteNameForMatch(route, cp, testParentRef(&listener1), match, 0)
	name2 := NewKongRouteNameForMatch(route, cp, testParentRef(&listener2), match, 0)

	assert.NotEqual(t, name1, name2)
	assert.Equal(t, name1, NewKongRouteNameForMatch(route, cp, testParentRef(&listener1), match, 0))
}

func TestNewKongRouteNameForMatch_WithoutParentRefKeepsLegacyFormat(t *testing.T) {
	route := testRoute("default", "test-route")
	cp := testControlPlaneRef("test-cp")
	matchType := gatewayv1.PathMatchPathPrefix
	match := gatewayv1.HTTPRouteMatch{
		Path: &gatewayv1.HTTPPathMatch{
			Type:  &matchType,
			Value: new("/"),
		},
	}

	result := NewKongRouteNameForMatch(route, cp, nil, match, 0)
	expected := "http.default-test-route.cp" + utils.Hash32(cp) + "." + utils.Hash32(match) + ".m00"
	assert.Equal(t, expected, result)
}

func TestNewKongRouteNameForTLSRouteRule_DiffersByParentRef(t *testing.T) {
	route := &gwtypes.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	cp := testControlPlaneRef("test-cp")
	rule := gwtypes.TLSRouteRule{
		BackendRefs: []gwtypes.BackendRef{{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name: "backend",
				Port: new(gatewayv1.PortNumber(443)),
			},
		}},
	}
	listener1 := gatewayv1.SectionName("listener-1")
	listener2 := gatewayv1.SectionName("listener-2")

	name1 := NewKongRouteNameForTLSRouteRule(route, cp, testParentRef(&listener1), rule)
	name2 := NewKongRouteNameForTLSRouteRule(route, cp, testParentRef(&listener2), rule)

	assert.NotEqual(t, name1, name2)
	assert.Equal(t, name1, NewKongRouteNameForTLSRouteRule(route, cp, testParentRef(&listener1), rule))
}

func TestNewKongRouteNameForTLSRouteRule_WithoutParentRefKeepsLegacyFormat(t *testing.T) {
	route := &gwtypes.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	cp := testControlPlaneRef("test-cp")
	rule := gwtypes.TLSRouteRule{
		BackendRefs: []gwtypes.BackendRef{{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name: "backend",
				Port: new(gatewayv1.PortNumber(443)),
			},
		}},
	}

	result := NewKongRouteNameForTLSRouteRule(route, cp, nil, rule)
	expected := "tls.default-test-route.cp" + utils.Hash32(cp) + "." + utils.Hash32(rule)
	assert.Equal(t, expected, result)
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
			result := NewKongPluginName(tt.filter, "default", "request-transformer")
			assert.NotEmpty(t, result)
			assert.True(t, strings.HasPrefix(result, "default.request-transformer."), "should start with plugin namespace.name prefix")
		})
	}
}

func TestNewKongPluginNameForFilters(t *testing.T) {
	headerModifier := gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
			Set: []gatewayv1.HTTPHeader{{Name: "X-Test", Value: "test-value"}},
		},
	}
	urlRewrite := gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterURLRewrite,
		URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
			Path: &gatewayv1.HTTPPathModifier{
				Type:               gatewayv1.PrefixMatchHTTPPathModifier,
				ReplacePrefixMatch: new("/echo"),
			},
		},
	}

	const ns, pluginName = "default", "request-transformer"

	tests := []struct {
		name    string
		filters []gatewayv1.HTTPRouteFilter
		// want is the name to compare against; wantEqual decides whether the generated name must
		// equal it or differ from it.
		want      string
		wantEqual bool
	}{
		{
			// Critical: renaming an existing single-filter plugin would momentarily leave two
			// plugins of the same type on a route, reintroducing the unique-plugin-per-entity error.
			name:      "single filter is backward compatible with NewKongPluginName",
			filters:   []gatewayv1.HTTPRouteFilter{headerModifier},
			want:      NewKongPluginName(headerModifier, ns, pluginName),
			wantEqual: true,
		},
		{
			name:      "merged name differs from the URLRewrite single-filter name",
			filters:   []gatewayv1.HTTPRouteFilter{urlRewrite, headerModifier},
			want:      NewKongPluginName(urlRewrite, ns, pluginName),
			wantEqual: false,
		},
		{
			name:      "merged name differs from the RequestHeaderModifier single-filter name",
			filters:   []gatewayv1.HTTPRouteFilter{urlRewrite, headerModifier},
			want:      NewKongPluginName(headerModifier, ns, pluginName),
			wantEqual: false,
		},
		{
			name:      "merged name changes when filter order changes",
			filters:   []gatewayv1.HTTPRouteFilter{urlRewrite, headerModifier},
			want:      NewKongPluginNameForFilters([]gatewayv1.HTTPRouteFilter{headerModifier, urlRewrite}, ns, pluginName),
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewKongPluginNameForFilters(tt.filters, ns, pluginName)

			// The name must be deterministic and carry the readable namespace.pluginType prefix.
			again := NewKongPluginNameForFilters(tt.filters, ns, pluginName)
			assert.Equal(t, got, again)
			assert.True(t, strings.HasPrefix(got, "default.request-transformer."))

			if tt.wantEqual {
				assert.Equal(t, tt.want, got)
			} else {
				assert.NotEqual(t, tt.want, got)
			}
		})
	}
}

func TestNewKongPluginBindingName(t *testing.T) {
	tests := []struct {
		name     string
		routeID  string
		pluginID string
		expected string
	}{
		{
			name:     "basic plugin binding name",
			routeID:  "default-test-route.cp12345678.ab123456",
			pluginID: "pl87654321",
			expected: "default-test-route.cp12345678.ab123456..pl87654321",
		},
		{
			name:     "empty route ID",
			routeID:  "",
			pluginID: "pl99887766",
			expected: "pl99887766",
		},
		{
			name:     "long route ID",
			routeID:  "very-long-namespace-name-very-long-route-name.cp12345678.ab123456",
			pluginID: "pl11223344",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongPluginBindingName(tt.routeID, tt.pluginID)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, tt.pluginID)
			if tt.routeID != "" {
				assert.Contains(t, result, tt.routeID)
			}
		})
	}
}

func TestNewKongTargetName(t *testing.T) {
	httpBR := func(name string) *gwtypes.HTTPBackendRef {
		return &gwtypes.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: gatewayv1.ObjectName(name),
					Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
				},
			},
		}
	}
	tlsBR := func(name string) *gwtypes.BackendRef {
		return &gwtypes.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Name: gatewayv1.ObjectName(name),
				Port: func() *gatewayv1.PortNumber { p := gatewayv1.PortNumber(8080); return &p }(),
			},
		}
	}

	t.Run("idempotent: same inputs produce the same name", func(t *testing.T) {
		br := httpBR("svc-a")
		first := NewKongTargetName("upstream", "10.0.0.1", 8080, br)
		second := NewKongTargetName("upstream", "10.0.0.1", 8080, br)
		assert.Equal(t, first, second)
	})

	t.Run("different backendRef same endpoint produces different name", func(t *testing.T) {
		// Critical for the name-reuse logic: when a rollout swaps the contributing
		// backendRef but points at the same pod IP, a fresh mint must produce a
		// different name so existingTargetNamesByAddress can distinguish the two.
		assert.NotEqual(t,
			NewKongTargetName("upstream", "10.0.0.1", 8080, httpBR("svc-a")),
			NewKongTargetName("upstream", "10.0.0.1", 8080, httpBR("svc-b")),
		)
	})

	t.Run("different endpoint same backendRef produces different name", func(t *testing.T) {
		br := httpBR("svc-a")
		assert.NotEqual(t,
			NewKongTargetName("upstream", "10.0.0.1", 8080, br),
			NewKongTargetName("upstream", "10.0.0.2", 8080, br),
		)
	})

	t.Run("different port same backendRef and endpoint produces different name", func(t *testing.T) {
		br := httpBR("svc-a")
		assert.NotEqual(t,
			NewKongTargetName("upstream", "10.0.0.1", 8080, br),
			NewKongTargetName("upstream", "10.0.0.1", 9090, br),
		)
	})

	t.Run("different upstream produces different name", func(t *testing.T) {
		br := httpBR("svc-a")
		assert.NotEqual(t,
			NewKongTargetName("upstream-x", "10.0.0.1", 8080, br),
			NewKongTargetName("upstream-y", "10.0.0.1", 8080, br),
		)
	})

	t.Run("name starts with upstream prefix", func(t *testing.T) {
		result := NewKongTargetName("cp12345678.ab123456", "192.168.1.100", 8080, httpBR("svc"))
		assert.True(t, strings.HasPrefix(result, "cp12345678.ab123456."), "name must start with upstream")
	})

	t.Run("name does not exceed kubernetes max length", func(t *testing.T) {
		longUpstream := strings.Repeat("very-long-upstream-segment-", 10)
		result := NewKongTargetName(longUpstream, "10.0.0.1", 8080, httpBR("svc"))
		assert.LessOrEqual(t, len(result), maxLen)
	})

	t.Run("TLSRoute BackendRef variant is idempotent", func(t *testing.T) {
		br := tlsBR("tls-backend")
		first := NewKongTargetName("upstream", "10.0.0.1", 443, br)
		second := NewKongTargetName("upstream", "10.0.0.1", 443, br)
		assert.Equal(t, first, second)
	})

	t.Run("TLSRoute BackendRef different service produces different name than HTTPBackendRef", func(t *testing.T) {
		// The TLS and HTTP variants hash different struct types, so names must not collide.
		assert.NotEqual(t,
			NewKongTargetName("upstream", "10.0.0.1", 8080, httpBR("svc-a")),
			NewKongTargetName("upstream", "10.0.0.1", 8080, tlsBR("svc-a")),
		)
	})
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
		result1 := NewKongUpstreamNameForHTTPRouteRule(route, cp, rule)
		result2 := NewKongUpstreamNameForHTTPRouteRule(route, cp, rule)
		assert.Equal(t, result1, result2)
	})

	t.Run("service name consistency", func(t *testing.T) {
		result1 := NewKongServiceNameForHTTPRouteRule(route, cp, rule)
		result2 := NewKongServiceNameForHTTPRouteRule(route, cp, rule)
		assert.Equal(t, result1, result2)
	})

	t.Run("plugin name consistency", func(t *testing.T) {
		result1 := NewKongPluginName(filter, "default", "request-transformer")
		result2 := NewKongPluginName(filter, "default", "request-transformer")
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
		listenerName string
		expected     string
	}{
		{
			name:         "short gateway and port",
			gatewayName:  "my-gateway",
			listenerPort: "443",
			listenerName: "https-1",
			expected:     "cert.my-gateway.443.https-1",
		},
		{
			name:         "gateway with namespace prefix",
			gatewayName:  "prod-api-gateway",
			listenerPort: "8443",
			listenerName: "tls-listener",
			expected:     "cert.prod-api-gateway.8443.tls-listener",
		},
		{
			name:         "different port",
			gatewayName:  "test-gw",
			listenerPort: "80",
			listenerName: "http",
			expected:     "cert.test-gw.80.http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewKongCertificateName(tt.gatewayName, tt.listenerPort, tt.listenerName)
			require.NotEmpty(t, result)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Run("same gateway and port with different listeners should differ", func(t *testing.T) {
		first := NewKongCertificateName("gateway", "443", "https-1")
		second := NewKongCertificateName("gateway", "443", "https-2")
		require.NotEqual(t, first, second)
	})

	t.Run("very long gateway name should hash", func(t *testing.T) {
		// Create a gateway name that when combined with "cert" and port exceeds 253 chars
		// "cert." (5) + longName + "." (1) + "443" (3) = need longName > 244 chars
		longGatewayName := strings.Repeat("very-long-gateway-name-segment-", 8) + "final-segment-to-exceed-limit"
		result := NewKongCertificateName(longGatewayName, "443", "https-listener")
		require.NotEmpty(t, result)
		require.True(t, strings.HasPrefix(result, namegenPrefix), "should hash when exceeding max length")
		require.LessOrEqual(t, len(result), maxLen)
	})
}
