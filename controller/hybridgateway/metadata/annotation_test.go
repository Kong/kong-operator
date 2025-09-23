package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/hybridgateway/route"
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
				consts.GatewayOperatorHybridRouteAnnotation:    route.HTTPRouteKey + "|" + "test-namespace/test-route",
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
				consts.GatewayOperatorHybridRouteAnnotation:    route.HTTPRouteKey + "|" + "test-namespace/test-route",
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
				consts.GatewayOperatorHybridRouteAnnotation:    route.HTTPRouteKey + "|" + "test-namespace/test-route",
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
				consts.GatewayOperatorHybridRouteAnnotation:    route.HTTPRouteKey + "|" + "apps/my-route",
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
		assert.Contains(t, routeAnnotation, expectedRouteKey.String())
		assert.Equal(t, route.HTTPRouteKey+"|"+expectedRouteKey.String(), routeAnnotation)
	})
}
