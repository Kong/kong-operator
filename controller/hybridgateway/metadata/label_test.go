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
		expected    map[string]string
		description string
	}{
		{
			name: "basic HTTPRoute",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
				consts.GatewayOperatorManagedByNameLabel:      "test-route",
				consts.GatewayOperatorManagedByNamespaceLabel: "test-namespace",
			},
			description: "should create correct labels for basic HTTPRoute",
		},
		{
			name: "HTTPRoute with different name and namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-api-route",
					Namespace: "production",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
				consts.GatewayOperatorManagedByNameLabel:      "my-api-route",
				consts.GatewayOperatorManagedByNamespaceLabel: "production",
			},
			description: "should handle different names and namespaces correctly",
		},
		{
			name: "HTTPRoute with empty name",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "test-namespace",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
				consts.GatewayOperatorManagedByNameLabel:      "",
				consts.GatewayOperatorManagedByNamespaceLabel: "test-namespace",
			},
			description: "should handle empty name correctly",
		},
		{
			name: "HTTPRoute with empty namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
				consts.GatewayOperatorManagedByNameLabel:      "test-route",
				consts.GatewayOperatorManagedByNamespaceLabel: "",
			},
			description: "should handle empty namespace correctly",
		},
		{
			name: "HTTPRoute with hyphenated names",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-word-route-name",
					Namespace: "my-app-namespace",
				},
			},
			expected: map[string]string{
				consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
				consts.GatewayOperatorManagedByNameLabel:      "multi-word-route-name",
				consts.GatewayOperatorManagedByNamespaceLabel: "my-app-namespace",
			},
			description: "should handle hyphenated names correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildLabels(tt.httpRoute)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestBuildLabelsConstants(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	result := BuildLabels(httpRoute)

	t.Run("includes managed-by label", func(t *testing.T) {
		managedBy, exists := result[consts.GatewayOperatorManagedByLabel]
		assert.True(t, exists, "should include managed-by label")
		assert.Equal(t, consts.HTTPRouteManagedByLabel, managedBy, "should use correct managed-by value")
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

	t.Run("returns exactly three labels", func(t *testing.T) {
		assert.Len(t, result, 3, "should return exactly three labels")
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
	result1 := BuildLabels(httpRoute)
	result2 := BuildLabels(httpRoute)

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
