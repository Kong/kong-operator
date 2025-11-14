package metadata

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
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
			expected:    true,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    true,
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
			expected: true,
		},
		{
			name: "strip-path empty value",
			annotations: map[string]string{
				"konghq.com/strip-path": "",
			},
			expected: true,
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
				consts.GatewayOperatorHybridRouteAnnotation:    "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "gateway-namespace/test-gateway",
			},
			description: "should use explicit parent namespace when provided",
		},
		{
			name: "with nil parent namespace",
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation:    "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "test-namespace/test-gateway",
			},
			description: "should use HTTPRoute namespace when parent namespace is nil",
		},
		{
			name: "with empty parent namespace",
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation:    "test-namespace/test-route",
				consts.GatewayOperatorHybridGatewaysAnnotation: "test-namespace/test-gateway",
			},
			description: "should use HTTPRoute namespace when parent namespace is empty",
		},
		{
			name: "with different route and gateway namespaces",
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation:    "apps/my-route",
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
		routeAnnotation := result[consts.GatewayOperatorHybridRouteAnnotation]

		expectedRouteKey := client.ObjectKeyFromObject(httpRoute)
		assert.Equal(t, expectedRouteKey.String(), routeAnnotation)
	})
}

func TestAppendHTTPRouteToAnnotation(t *testing.T) {
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
				consts.GatewayOperatorHybridRouteAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route,test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "route already exists in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route3",
					Namespace: "ns3",
				},
			},
			expectedAnnotation: "ns1/route1,ns2/route2,ns3/route3",
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
				for k, v := range tt.existingAnnotations {
					obj.Annotations[k] = v
				}
			}

			modified, err := am.AppendHTTPRouteToAnnotation(obj, tt.httpRoute)
			require.NoError(t, err)

			assert.Equal(t, tt.expectModification, modified)
			actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestRemoveHTTPRouteFromAnnotation(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	tests := []struct {
		name                    string
		existingAnnotations     map[string]string
		httpRoute               *gwtypes.HTTPRoute
		expectedAnnotation      string
		expectModification      bool
		expectAnnotationDeleted bool
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "test-namespace/test-route,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			httpRoute: &gwtypes.HTTPRoute{
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
				for k, v := range tt.existingAnnotations {
					obj.Annotations[k] = v
				}
			}

			modified, err := am.RemoveHTTPRouteFromAnnotation(obj, tt.httpRoute)
			require.NoError(t, err)

			assert.Equal(t, tt.expectModification, modified)

			if tt.expectAnnotationDeleted {
				_, exists := obj.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
				assert.False(t, exists, "annotation should be deleted")
			} else if tt.expectedAnnotation != "" {
				actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
				assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
			}
		})
	}
}

func TestContainsHTTPRoute(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	tests := []struct {
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
				consts.GatewayOperatorHybridRouteAnnotation: "",
			},
			expected: false,
		},
		{
			name: "route exists",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "test-namespace/test-route",
			},
			expected: true,
		},
		{
			name: "route exists among others",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			expected: true,
		},
		{
			name: "different route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "other-namespace/other-route",
			},
			expected: false,
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

			result := am.ContainsHTTPRoute(obj, httpRoute)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetHTTPRoutes(t *testing.T) {
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
				consts.GatewayOperatorHybridRouteAnnotation: "",
			},
			expected: []string{},
		},
		{
			name: "single route",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "test-namespace/test-route",
			},
			expected: []string{"test-namespace/test-route"},
		},
		{
			name: "multiple routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			},
			expected: []string{"ns1/route1", "ns2/route2", "ns3/route3"},
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

			routes, err := am.GetHTTPRoutes(obj)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, routes)
		})
	}
}

func TestSetHTTPRoutes(t *testing.T) {
	logger := logr.Discard()
	am := NewAnnotationManager(logger)

	httpRoute1 := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "ns1"},
	}
	httpRoute2 := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route2", Namespace: "ns2"},
	}

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		httpRoutes          []*gwtypes.HTTPRoute
		expectedAnnotation  string
		expectModification  bool
	}{
		{
			name:                "set on empty object",
			existingAnnotations: nil,
			httpRoutes:          []*gwtypes.HTTPRoute{httpRoute1},
			expectedAnnotation:  "ns1/route1",
			expectModification:  true,
		},
		{
			name: "set same routes - no change",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1",
			},
			httpRoutes:         []*gwtypes.HTTPRoute{httpRoute1},
			expectedAnnotation: "ns1/route1",
			expectModification: false,
		},
		{
			name: "replace routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1",
			},
			httpRoutes:         []*gwtypes.HTTPRoute{httpRoute2},
			expectedAnnotation: "ns2/route2",
			expectModification: true,
		},
		{
			name: "set multiple routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "old/route",
			},
			httpRoutes:         []*gwtypes.HTTPRoute{httpRoute1, httpRoute2},
			expectedAnnotation: "ns1/route1,ns2/route2",
			expectModification: true,
		},
		{
			name: "clear routes",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRouteAnnotation: "ns1/route1",
			},
			httpRoutes:         []*gwtypes.HTTPRoute{},
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
				for k, v := range tt.existingAnnotations {
					obj.Annotations[k] = v
				}
			}

			modified := am.SetHTTPRoutes(obj, tt.httpRoutes)
			assert.Equal(t, tt.expectModification, modified)

			if tt.expectedAnnotation == "" {
				_, exists := obj.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
				assert.False(t, exists, "annotation should be deleted when empty")
			} else {
				actualAnnotation := obj.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
				assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
			}
		})
	}
}

// TestGenericObjectTypes tests that the annotation manager works with different Kubernetes object types
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
			modified, err := am.AppendHTTPRouteToAnnotation(obj, httpRoute)
			require.NoError(t, err)
			assert.True(t, modified)

			// Test contains
			contains := am.ContainsHTTPRoute(obj, httpRoute)
			assert.True(t, contains)

			// Test getting routes
			routes, err := am.GetHTTPRoutes(obj)
			require.NoError(t, err)
			assert.Equal(t, []string{"test-namespace/test-route"}, routes)

			// Test removing
			modified, err = am.RemoveHTTPRouteFromAnnotation(obj, httpRoute)
			require.NoError(t, err)
			assert.True(t, modified)

			// Verify removed
			contains = am.ContainsHTTPRoute(obj, httpRoute)
			assert.False(t, contains)
		})
	}
}
