package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

func TestBuildLabels(t *testing.T) {
	tests := []struct {
		name        string
		httpRoute   *gwtypes.HTTPRoute
		parentRef   *gwtypes.ParentReference
		expected    map[string]string
		description string
	}{
		{
			name: "basic HTTPRoute without parentRef",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind: "HTTPRoute",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			parentRef: nil,
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          "HTTPRoute",
				consts.GatewayOperatorManagedByNameLabel:      "test-route",
				consts.GatewayOperatorManagedByNamespaceLabel: "test-namespace",
			},
			description: "should create correct labels for basic HTTPRoute without parentRef",
		},
		{
			name: "HTTPRoute with parentRef",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind: "HTTPRoute",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-api-route",
					Namespace: "production",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:               "HTTPRoute",
				consts.GatewayOperatorManagedByNameLabel:           "my-api-route",
				consts.GatewayOperatorManagedByNamespaceLabel:      "production",
				consts.GatewayOperatorHybridGatewaysNameLabel:      "test-gateway",
				consts.GatewayOperatorHybridGatewaysNamespaceLabel: "production",
			},
			description: "should handle HTTPRoute with parentRef correctly",
		},
		{
			name: "HTTPRoute with parentRef and explicit namespace",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind: "HTTPRoute",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cross-ns-route",
					Namespace: "route-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "gateway-name",
				Namespace: func() *gwtypes.Namespace { ns := gwtypes.Namespace("gateway-namespace"); return &ns }(),
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:               "HTTPRoute",
				consts.GatewayOperatorManagedByNameLabel:           "cross-ns-route",
				consts.GatewayOperatorManagedByNamespaceLabel:      "route-namespace",
				consts.GatewayOperatorHybridGatewaysNameLabel:      "gateway-name",
				consts.GatewayOperatorHybridGatewaysNamespaceLabel: "gateway-namespace",
			},
			description: "should handle parentRef with explicit namespace correctly",
		},
		{
			name: "HTTPRoute with empty name without parentRef",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind: "HTTPRoute",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "test-namespace",
				},
			},
			parentRef: nil,
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          "HTTPRoute",
				consts.GatewayOperatorManagedByNameLabel:      "",
				consts.GatewayOperatorManagedByNamespaceLabel: "test-namespace",
			},
			description: "should handle empty name correctly",
		},
		{
			name: "HTTPRoute with empty namespace without parentRef",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: metav1.TypeMeta{
					Kind: "HTTPRoute",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "",
				},
			},
			parentRef: nil,
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          "HTTPRoute",
				consts.GatewayOperatorManagedByNameLabel:      "test-route",
				consts.GatewayOperatorManagedByNamespaceLabel: "",
			},
			description: "should handle empty namespace correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildLabels(tt.httpRoute, tt.parentRef)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestBuildLabelsConstants(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind: "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	result := BuildLabels(httpRoute, nil)

	t.Run("includes managed-by label", func(t *testing.T) {
		managedBy, exists := result[consts.GatewayOperatorManagedByLabel]
		assert.True(t, exists, "should include managed-by label")
		assert.Equal(t, "HTTPRoute", managedBy, "should use correct managed-by value")
	})

	t.Run("includes name label", func(t *testing.T) {
		name, exists := result[consts.GatewayOperatorManagedByNameLabel]
		assert.True(t, exists, "should include name label")
		assert.Equal(t, httpRoute.GetName(), name, "should use HTTPRoute name")
	})

	t.Run("includes namespace label", func(t *testing.T) {
		namespace, exists := result[consts.GatewayOperatorManagedByNamespaceLabel]
		assert.True(t, exists, "should include namespace label")
		assert.Equal(t, httpRoute.GetNamespace(), namespace, "should use HTTPRoute namespace")
	})

	t.Run("returns exactly three labels when parentRef is nil", func(t *testing.T) {
		assert.Len(t, result, 3, "should return exactly three labels when parentRef is nil")
	})
}

func TestBuildLabelsImmutability(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	// Call the function multiple times
	result1 := BuildLabels(httpRoute, nil)
	result2 := BuildLabels(httpRoute, nil)

	t.Run("returns consistent results", func(t *testing.T) {
		assert.Equal(t, result1, result2, "should return consistent results across multiple calls")
	})

	t.Run("returns independent maps", func(t *testing.T) {
		// Modify one map and ensure the other is not affected
		result1["test-key"] = "test-value"
		_, exists := result2["test-key"]
		assert.False(t, exists, "modifying one result should not affect another")
	})
}

func TestLabelSelectorForOwnedResources(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind: "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	t.Run("without parentRef", func(t *testing.T) {
		selector := LabelSelectorForOwnedResources(httpRoute, nil)
		assert.NotNil(t, selector, "should return a valid selector")
	})

	t.Run("with parentRef", func(t *testing.T) {
		parentRef := &gwtypes.ParentReference{
			Name: "test-gateway",
		}
		selector := LabelSelectorForOwnedResources(httpRoute, parentRef)
		assert.NotNil(t, selector, "should return a valid selector")
	})
}
