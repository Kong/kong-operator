package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestNamespaceFromParentRef(t *testing.T) {
	testNamespace := gwtypes.Namespace("test-namespace")
	differentNamespace := gwtypes.Namespace("different-namespace")
	emptyNamespace := gwtypes.Namespace("")

	tests := []struct {
		name        string
		obj         metav1.Object
		parentRef   *gwtypes.ParentReference
		expected    string
		description string
	}{
		{
			name: "parentRef with explicit namespace",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: &differentNamespace,
			},
			expected:    "different-namespace",
			description: "should return the namespace from parentRef when explicitly set",
		},
		{
			name: "parentRef with nil namespace",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: nil,
			},
			expected:    "test-namespace",
			description: "should return the object's namespace when parentRef.Namespace is nil",
		},
		{
			name: "parentRef with empty namespace string",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: &emptyNamespace,
			},
			expected:    "test-namespace",
			description: "should return the object's namespace when parentRef.Namespace is empty string",
		},
		{
			name: "object in default namespace with parentRef namespace",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "prod-gateway",
				Namespace: &testNamespace,
			},
			expected:    "test-namespace",
			description: "should return parentRef namespace even when object is in default namespace",
		},
		{
			name: "object with no namespace and nil parentRef namespace",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: nil,
			},
			expected:    "",
			description: "should return empty string when both object and parentRef have no namespace",
		},
		{
			name: "parentRef with same namespace as object",
			obj: &metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "test-gateway",
				Namespace: &testNamespace,
			},
			expected:    "test-namespace",
			description: "should return namespace correctly when parentRef and object share the same namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NamespaceFromParentRef(tt.obj, tt.parentRef)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestNamespaceFromParentRefWithHTTPRoute(t *testing.T) {
	crossNamespace := gwtypes.Namespace("gateway-namespace")

	tests := []struct {
		name        string
		httpRoute   *gwtypes.HTTPRoute
		parentRef   *gwtypes.ParentReference
		expected    string
		description string
	}{
		{
			name: "HTTPRoute with cross-namespace gateway reference",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "api-route",
					Namespace: "app-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name:      "shared-gateway",
				Namespace: &crossNamespace,
			},
			expected:    "gateway-namespace",
			description: "should support cross-namespace references",
		},
		{
			name: "HTTPRoute with same-namespace gateway reference",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "api-route",
					Namespace: "app-namespace",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name: "local-gateway",
			},
			expected:    "app-namespace",
			description: "should default to HTTPRoute namespace when parentRef namespace is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NamespaceFromParentRef(&tt.httpRoute.ObjectMeta, tt.parentRef)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
