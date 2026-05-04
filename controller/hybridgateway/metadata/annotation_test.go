package metadata

import (
	"fmt"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	httpRouteTypeMeta = metav1.TypeMeta{
		Kind:       kindHTTPRoute,
		APIVersion: "gateway.networking.k8s.io/v1",
	}
	tlsRouteTypeMeta = metav1.TypeMeta{
		Kind:       "TLSRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
	}
)

func TestExtractStripPath(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "strip-path true",
			annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
			expected: true,
		},
		{
			name: "strip-path false",
			annotations: map[string]string{
				"konghq.com/strip-path": "false",
			},
			expected: false,
		},
		{
			name: "strip-path invalid value",
			annotations: map[string]string{
				"konghq.com/strip-path": "invalid",
			},
			expected: false,
		},
		{
			name: "strip-path empty value",
			annotations: map[string]string{
				"konghq.com/strip-path": "",
			},
			expected: false,
		},
		{
			name: "other annotations present",
			annotations: map[string]string{
				"other-annotation":      "value",
				"konghq.com/strip-path": "false",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractStripPath(tt.annotations)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPreserveHost(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    true,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    true,
		},
		{
			name: "preserve-host true",
			annotations: map[string]string{
				"konghq.com/preserve-host": "true",
			},
			expected: true,
		},
		{
			name: "preserve-host false",
			annotations: map[string]string{
				"konghq.com/preserve-host": "false",
			},
			expected: false,
		},
		{
			name: "preserve-host invalid value",
			annotations: map[string]string{
				"konghq.com/preserve-host": "invalid",
			},
			expected: true,
		},
		{
			name: "preserve-host empty value",
			annotations: map[string]string{
				"konghq.com/preserve-host": "",
			},
			expected: true,
		},
		{
			name: "other annotations present",
			annotations: map[string]string{
				"other-annotation":         "value",
				"konghq.com/preserve-host": "false",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPreserveHost(tt.annotations)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		httpRoute   *gwtypes.HTTPRoute
		parentRef   *gwtypes.ParentReference
		expected    map[string]string
		description string
	}{
		{
			name: "with explicit parent namespace",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: func() *gwtypes.Namespace { ns := gwtypes.Namespace("gateway-namespace"); return &ns }(),
			},
			expected: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation:        "gateway-namespace/test-gateway",
			},
			description: "should use explicit parent namespace when provided",
		},
		{
			name: "with nil parent namespace",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: nil,
			},
			expected: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation:        "test-namespace/test-gateway",
			},
			description: "should use HTTPRoute namespace when parent namespace is nil",
		},
		{
			name: "with empty parent namespace",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: func() *gwtypes.Namespace { ns := gwtypes.Namespace(""); return &ns }(),
			},
			expected: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation:        "test-namespace/test-gateway",
			},
			description: "should use HTTPRoute namespace when parent namespace is empty",
		},
		{
			name: "with different route and gateway namespaces",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-route",
					Namespace: "apps",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "my-gateway",
				Namespace: func() *gwtypes.Namespace { ns := gwtypes.Namespace("infrastructure"); return &ns }(),
			},
			expected: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "apps/my-route",
				consts.GatewayOperatorHybridGatewaysAnnotation:        "infrastructure/my-gateway",
			},
			description: "should handle different namespaces correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildAnnotations(tt.httpRoute, tt.parentRef)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestBuildAnnotationsObjectKeyCreation(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	t.Run("creates correct ObjectKey for gateway", func(t *testing.T) {
		parentRef := &gwtypes.ParentReference{
			Name:      "test-gateway",
			Namespace: func() *gwtypes.Namespace { ns := gwtypes.Namespace("gateway-namespace"); return &ns }(),
		}

		result := BuildAnnotations(httpRoute, parentRef)
		gatewayAnnotation := result[consts.GatewayOperatorHybridGatewaysAnnotation]

		expectedGatewayKey := client.ObjectKey{
			Name:      "test-gateway",
			Namespace: "gateway-namespace",
		}
		assert.Equal(t, expectedGatewayKey.String(), gatewayAnnotation)
	})

	t.Run("creates correct ObjectKey for HTTPRoute", func(t *testing.T) {
		parentRef := &gwtypes.ParentReference{
			Name:      "test-gateway",
			Namespace: nil,
		}

		result := BuildAnnotations(httpRoute, parentRef)
		routeAnnotation := result[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]

		expectedRouteKey := client.ObjectKeyFromObject(httpRoute)
		assert.Equal(t, expectedRouteKey.String(), routeAnnotation)
	})
}

func TestAppendRouteToAnnotation(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		httpRoute           *gwtypes.HTTPRoute
		expectedAnnotation  string
		expectModification  bool
	}{
		{
			name:                "no existing annotations",
			existingAnnotations: nil,
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "empty hybrid-route annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "existing different route in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route,test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "route already exists in annotation without kind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: false,
		},
		{
			name: "multiple existing routes, adding new one",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route3",
					Namespace: "ns3",
				},
			},
			expectedAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			expectModification: true,
		},
		{
			name: "same namespace and name but different kinds",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: make(map[string]string),
				},
			}

			// Copy existing annotations
			if tt.existingAnnotations != nil {
				maps.Copy(obj.Annotations, tt.existingAnnotations)
			}

			modified := am.AppendRouteToAnnotation(obj, tt.httpRoute)

			assert.Equal(t, tt.expectModification, modified)
			actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestRemoveRouteFromAnnotation(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                    string
		existingAnnotations     map[string]string
		route                   client.Object
		expectedAnnotation      string
		expectModification      bool
		expectAnnotationDeleted bool
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			route: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "",
			expectModification: false,
		},
		{
			name: "route not in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			route: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route",
			expectModification: false,
		},
		{
			name: "remove only route - annotation should be deleted",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			route: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation:      "",
			expectModification:      true,
			expectAnnotationDeleted: true,
		},
		{
			name: "remove first route from multiple",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route,ns2/route2",
			},
			route: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns2/route2",
			expectModification: true,
		},
		{
			name: "remove middle route from multiple",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			route: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns1/route1,ns3/route3",
			expectModification: true,
		},
		{
			name: "does not remove if kind does not match",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation:  "other-namespace/other-route",
			},
			route: &gwtypes.TLSRoute{
				TypeMeta: tlsRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route",
			expectModification: false,
		},
		{
			name: "remove middle route from multiple - TLSRoute",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			route: &gwtypes.TLSRoute{
				TypeMeta: tlsRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns1/route1,ns3/route3",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: make(map[string]string),
				},
			}

			// Copy existing annotations
			if tt.existingAnnotations != nil {
				maps.Copy(obj.Annotations, tt.existingAnnotations)
			}

			modified := am.RemoveRouteFromAnnotation(obj, tt.route)

			assert.Equal(t, tt.expectModification, modified)

			routeAnnotaionKey := am.RouteAnnotationKeyForKind(tt.route.GetObjectKind().GroupVersionKind().Kind)

			if tt.expectAnnotationDeleted {
				_, exists := obj.Annotations[routeAnnotaionKey]
				assert.False(t, exists, "annotation should be deleted")
			} else if tt.expectedAnnotation != "" {
				actualAnnotation := obj.Annotations[routeAnnotaionKey]
				assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
			}
		})
	}
}

func TestContainsRoute(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	tlsRoute := &gwtypes.TLSRoute{
		TypeMeta: tlsRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	testsHTTPRoute := []struct {
		name                string
		existingAnnotations map[string]string
		expected            bool
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			expected:            false,
		},
		{
			name: "empty annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			},
			expected: false,
		},
		{
			name: "route exists",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "route exists among others",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			expected: true,
		},
		{
			name: "different route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			expected: false,
		},
		{
			name: "annotation with kind but the kind is different",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "test-namespace/test-route",
			},
			expected: false,
		},
	}

	testsTLSRoute := []struct {
		name                string
		existingAnnotations map[string]string
		expected            bool
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			expected:            false,
		},
		{
			name: "empty annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "",
			},
			expected: false,
		},
		{
			name: "route exists",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "route exists among others",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			expected: true,
		},
		{
			name: "different route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "other-namespace/other-route",
			},
			expected: false,
		},
	}

	for _, tt := range testsHTTPRoute {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: tt.existingAnnotations,
				},
			}

			result := am.ContainsRoute(obj, httpRoute)
			assert.Equal(t, tt.expected, result)
		})
	}

	for _, tt := range testsTLSRoute {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: tt.existingAnnotations,
				},
			}

			result := am.ContainsRoute(obj, tlsRoute)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetRoutesWithKind(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                string
		routeKind           string
		existingAnnotations map[string]string
		expected            []string
	}{
		{
			name:                "no annotations",
			routeKind:           kindHTTPRoute,
			existingAnnotations: nil,
			expected:            []string{},
		},
		{
			name:      "empty annotation",
			routeKind: kindHTTPRoute,
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			},
			expected: []string{},
		},
		{
			name:      "single route",
			routeKind: kindHTTPRoute,
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			expected: []string{"test-namespace/test-route"},
		},
		{
			name:      "multiple routes",
			routeKind: kindHTTPRoute,
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			},
			expected: []string{"ns1/route1", "ns2/route2", "ns3/route3"},
		},
		{
			name:      "single route - TLSRoute",
			routeKind: kindTLSRoute,
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "test-namespace/test-route",
			},
			expected: []string{"test-namespace/test-route"},
		},
		{
			name:      "multiple routes with different kinds",
			routeKind: kindTLSRoute,
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation:  "ns2/route2,ns3/route3",
			},
			expected: []string{"ns2/route2", "ns3/route3"},
		},
		{
			name:      "unsupported route kind",
			routeKind: "unsupportedKind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation:  "ns2/route2,ns3/route3",
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: tt.existingAnnotations,
				},
			}

			routes := am.GetRoutesWithKind(obj, tt.routeKind)
			assert.Equal(t, tt.expected, routes)
		})
	}
}

func TestSetRoutesWithKind(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		routeKind           string
		routes              []string
		expectedAnnotation  string
		expectModification  bool
	}{
		{
			name:                "set on empty object",
			existingAnnotations: nil,
			routeKind:           kindHTTPRoute,
			routes:              []string{"ns1/route1"},
			expectedAnnotation:  "ns1/route1",
			expectModification:  true,
		},
		{
			name: "set same routes - no change",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1",
			},
			routeKind:          kindHTTPRoute,
			routes:             []string{"ns1/route1"},
			expectedAnnotation: "ns1/route1",
			expectModification: false,
		},
		{
			name: "replace routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1",
			},
			routeKind:          kindHTTPRoute,
			routes:             []string{"ns2/route2"},
			expectedAnnotation: "ns2/route2",
			expectModification: true,
		},
		{
			name: "set multiple routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "old/route",
			},
			routeKind:          kindHTTPRoute,
			routes:             []string{"ns1/route1", "ns2/route2"},
			expectedAnnotation: "ns1/route1,ns2/route2",
			expectModification: true,
		},
		{
			name: "clear routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1",
			},
			routeKind:          kindHTTPRoute,
			routes:             []string{},
			expectedAnnotation: "",
			expectModification: true,
		},
		{
			name: "set same routes - TLSRoute",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation: "ns1/route1",
			},
			routeKind:          kindTLSRoute,
			routes:             []string{"ns1/route1"},
			expectedAnnotation: "ns1/route1",
			expectModification: false,
		},
		{
			name: "replace routes - TLSRoute",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1",
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation:  "ns2/route2",
			},
			routeKind:          kindTLSRoute,
			routes:             []string{"ns1/route1"},
			expectedAnnotation: "ns1/route1",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-object",
					Namespace:   "test-namespace",
					Annotations: make(map[string]string),
				},
			}

			// Copy existing annotations
			if tt.existingAnnotations != nil {
				maps.Copy(obj.Annotations, tt.existingAnnotations)
			}

			modified := am.SetRoutesWithKind(obj, tt.routeKind, tt.routes)
			assert.Equal(t, tt.expectModification, modified)

			routeKey := am.RouteAnnotationKeyForKind(tt.routeKind)
			assert.NotEmpty(t, routeKey)
			if tt.expectedAnnotation == "" {
				_, exists := obj.Annotations[routeKey]
				assert.False(t, exists, "annotation should be deleted when empty")
			} else {
				actualAnnotation := obj.Annotations[routeKey]
				assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
			}
		})
	}
}

func TestExtractProtocol(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    string
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    "",
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    "",
		},
		{
			name: "protocol annotation present",
			annotations: map[string]string{
				"konghq.com/protocol": "https",
			},
			expected: "https",
		},
		{
			name: "protocol annotation empty value",
			annotations: map[string]string{
				"konghq.com/protocol": "",
			},
			expected: "",
		},
		{
			name: "other annotations present without protocol",
			annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
			expected: "",
		},
		{
			name: "grpc protocol",
			annotations: map[string]string{
				"konghq.com/protocol": "grpc",
			},
			expected: "grpc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractProtocol(tt.annotations)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestExtractConnectTimeout(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    *int64
	}{
		{name: "nil annotations", annotations: nil, expected: nil},
		{name: "empty annotations", annotations: map[string]string{}, expected: nil},
		{name: "valid timeout", annotations: map[string]string{"konghq.com/connect-timeout": "5000"}, expected: int64Ptr(5000)},
		{name: "zero timeout", annotations: map[string]string{"konghq.com/connect-timeout": "0"}, expected: int64Ptr(0)},
		{name: "negative invalid", annotations: map[string]string{"konghq.com/connect-timeout": "-1"}, expected: nil},
		{name: "non-numeric", annotations: map[string]string{"konghq.com/connect-timeout": "abc"}, expected: nil},
		{name: "empty value", annotations: map[string]string{"konghq.com/connect-timeout": ""}, expected: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ExtractConnectTimeout(tt.annotations)
			if tt.expected == nil {
				assert.Nil(t, v)
			} else {
				require.NotNil(t, v)
				assert.Equal(t, *tt.expected, *v)
			}
		})
	}
}

func TestIsValidProtocol(t *testing.T) {
	validProtocols := []string{"http", "https", "grpc", "grpcs", "ws", "wss", "tls", "tcp", "tls_passthrough"}
	for _, p := range validProtocols {
		t.Run("valid_"+p, func(t *testing.T) {
			assert.True(t, IsValidProtocol(p))
		})
	}

	invalidProtocols := []string{"", "HTTP", "HTTPS", "invalid", "ftp", "udps", "unix"}
	for _, p := range invalidProtocols {
		name := p
		if name == "" {
			name = "empty"
		}
		t.Run("invalid_"+name, func(t *testing.T) {
			assert.False(t, IsValidProtocol(p))
		})
	}
}

// TestGenericObjectTypes tests that the annotation manager works with different Kubernetes object types.
func TestGenericObjectTypes(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	// Test with different Kubernetes object types
	objects := []metav1.Object{
		&configurationv1alpha1.KongUpstream{
			ObjectMeta: metav1.ObjectMeta{Name: "test-upstream", Namespace: "test-namespace"},
		},
		&configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{Name: "test-service", Namespace: "test-namespace"},
		},
		&configurationv1alpha1.KongRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "test-route-obj", Namespace: "test-namespace"},
		},
		&configurationv1alpha1.KongTarget{
			ObjectMeta: metav1.ObjectMeta{Name: "test-target", Namespace: "test-namespace"},
		},
	}

	for _, obj := range objects {
		t.Run(fmt.Sprintf("test_%T", obj), func(t *testing.T) {
			// Test appending
			modified := am.AppendRouteToAnnotation(obj, httpRoute)
			assert.True(t, modified)

			// Test contains
			contains := am.ContainsRoute(obj, httpRoute)
			assert.True(t, contains)

			// Test getting routes
			routes := am.GetRoutesWithKind(obj, kindHTTPRoute)
			assert.Equal(t, []string{"test-namespace/test-route"}, routes)

			// Test removing
			modified = am.RemoveRouteFromAnnotation(obj, httpRoute)
			assert.True(t, modified)

			// Verify removed
			contains = am.ContainsRoute(obj, httpRoute)
			assert.False(t, contains)
		})
	}
}
