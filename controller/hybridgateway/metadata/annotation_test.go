package metadata

import (
	"fmt"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
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
				consts.GatewayOperatorHybridRoutesAnnotation:   "HTTPRoute/test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "gateway-namespace/test-gateway",
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
				consts.GatewayOperatorHybridRoutesAnnotation:   "HTTPRoute/test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "test-namespace/test-gateway",
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
				consts.GatewayOperatorHybridRoutesAnnotation:   "HTTPRoute/test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "test-namespace/test-gateway",
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
				consts.GatewayOperatorHybridRoutesAnnotation:   "HTTPRoute/apps/my-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "infrastructure/my-gateway",
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
		routeAnnotation := result[consts.GatewayOperatorHybridRoutesAnnotation]

		expectedRouteKey := client.ObjectKeyFromObject(httpRoute)
		assert.Equal(t, kindHTTPRoute+"/"+expectedRouteKey.String(), routeAnnotation)
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
			expectedAnnotation: "HTTPRoute/test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "empty hybrid-route annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "HTTPRoute/test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "existing different route in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "HTTPRoute/other-namespace/other-route,HTTPRoute/test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "route already exists in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "HTTPRoute/test-namespace/test-route",
			expectModification: false,
		},
		{
			name: "multiple existing routes, adding new one",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/ns1/route1,HTTPRoute/ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route3",
					Namespace: "ns3",
				},
			},
			expectedAnnotation: "HTTPRoute/ns1/route1,HTTPRoute/ns2/route2,HTTPRoute/ns3/route3",
			expectModification: true,
		},
		{
			name: "same namespace and name but different kinds",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "TLSRoute/test-namespace/test-route,HTTPRoute/test-namespace/test-route",
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
			actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestRemoveRouteFromAnnotation_LegacyFormat(t *testing.T) {
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
				consts.GatewayOperatorHybridRoutesAnnotation: "other-namespace/other-route",
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
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
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
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route,ns2/route2",
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
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
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
			// No kind implies that the kind of parent route is HTTPRoute so TLSRoute with the same namespace and name should not trigger the removal.
			name: "does not remove if kind does not match",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
			},
			route: &gwtypes.TLSRoute{
				TypeMeta: tlsRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: false,
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

			if tt.expectAnnotationDeleted {
				_, exists := obj.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
				assert.False(t, exists, "annotation should be deleted")
			} else if tt.expectedAnnotation != "" {
				actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
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
				consts.GatewayOperatorHybridRoutesAnnotation: "",
			},
			expected: false,
		},
		{
			name: "route exists",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "route exists among others",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			expected: true,
		},
		{
			name: "different route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "other-namespace/other-route",
			},
			expected: false,
		},
		{
			name: "annotation with kind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "annotation with kind but other route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/other-namespace/test-route",
			},
			expected: false,
		},
		{
			name: "annotation with kind but the kind is different",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/test-namespace/test-route",
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
				consts.GatewayOperatorHybridRoutesAnnotation: "",
			},
			expected: false,
		},
		{
			name: "route exists",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "route exists among others",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/ns1/route1,TLSRoute/test-namespace/test-route,HTTPRoute/ns3/route3",
			},
			expected: true,
		},
		{
			name: "different route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/other-namespace/other-route",
			},
			expected: false,
		},
		{
			name: "annotation without kind implies HTTPRoute",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
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

func TestGetRoutes(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		expected            []string
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			expected:            []string{},
		},
		{
			name: "empty annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "",
			},
			expected: []string{},
		},
		{
			name: "single route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
			},
			expected: []string{"HTTPRoute/test-namespace/test-route"},
		},
		{
			name: "multiple routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			},
			expected: []string{"HTTPRoute/ns1/route1", "HTTPRoute/ns2/route2", "HTTPRoute/ns3/route3"},
		},
		{
			name: "single route with kind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "TLSRoute/test-namespace/test-route",
			},
			expected: []string{"TLSRoute/test-namespace/test-route"},
		},
		{
			name: "multiple routes with different kinds and without kind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "HTTPRoute/ns1/route1,TLSRoute/ns2/route2,ns3/route3",
			},
			expected: []string{"HTTPRoute/ns1/route1", "TLSRoute/ns2/route2", "HTTPRoute/ns3/route3"},
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

			routes := am.GetRoutes(obj)
			assert.Equal(t, tt.expected, routes)
		})
	}
}

func TestSetRoutes(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		routes              []string
		expectedAnnotation  string
		expectModification  bool
	}{
		{
			name:                "set on empty object",
			existingAnnotations: nil,
			routes:              []string{"ns1/route1"},
			expectedAnnotation:  "ns1/route1",
			expectModification:  true,
		},
		{
			name: "set same routes - no change",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1",
			},
			routes:             []string{"ns1/route1"},
			expectedAnnotation: "ns1/route1",
			expectModification: false,
		},
		{
			name: "replace routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1",
			},
			routes:             []string{"ns2/route2"},
			expectedAnnotation: "ns2/route2",
			expectModification: true,
		},
		{
			name: "set multiple routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "old/route",
			},
			routes:             []string{"ns1/route1", "ns2/route2"},
			expectedAnnotation: "ns1/route1,ns2/route2",
			expectModification: true,
		},
		{
			name: "clear routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1",
			},
			routes:             []string{},
			expectedAnnotation: "",
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

			modified := am.SetRoutes(obj, tt.routes)
			assert.Equal(t, tt.expectModification, modified)

			if tt.expectedAnnotation == "" {
				_, exists := obj.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
				assert.False(t, exists, "annotation should be deleted when empty")
			} else {
				actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
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
			routes := am.GetRoutes(obj)
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
