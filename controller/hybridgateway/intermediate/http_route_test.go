package intermediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestNewHTTPRouteRepresentation_StripPath(t *testing.T) {
	tests := []struct {
		name     string
		route    *gwtypes.HTTPRoute
		expected bool
	}{
		{
			name: "HTTPRoute without strip-path annotation",
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expected: true,
		},
		{
			name: "HTTPRoute with strip-path true",
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"konghq.com/strip-path": "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "HTTPRoute with strip-path false",
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						"konghq.com/strip-path": "false",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repr := NewHTTPRouteRepresentation(tt.route)
			assert.Equal(t, tt.expected, repr.StripPath)
		})
	}
}
