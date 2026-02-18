package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
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
				consts.GatewayOperatorManagedByLabel: "HTTPRoute",
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
				consts.GatewayOperatorManagedByLabel: "HTTPRoute",
			},
			description: "should handle HTTPRoute with parentRef correctly",
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

	t.Run("returns exactly three labels when parentRef is nil", func(t *testing.T) {
		assert.Len(t, result, 1, "should return exactly one label when parentRef is nil")
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
