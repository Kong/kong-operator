package translator

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestRefChecker_IsRefAllowedByGrant(t *testing.T) {
	testcases := []struct {
		name        string
		route       *gwtypes.HTTPRoute
		backendRef  gwtypes.BackendRef
		allowedRefs map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo
		expected    bool
	}{
		{
			name: "allowed by grant",
			route: &gwtypes.HTTPRoute{
				Namespace: "example-namespace",
				Name:      "example-name",
			},
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Kind:  new(gwtypes.Kind("example-kind")),
					Group: new(gwtypes.Group("example-group")),
					Name:  "example-name",
				},
			},
			allowedRefs: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				"example-namespace": {
					{
						Kind:  gwtypes.Kind("example-kind"),
						Group: gwtypes.Group("example-group"),
						Name:  new(gwtypes.ObjectName("example-name")),
					},
				},
			},
			expected: true,
		},
		{
			name: "allowed by grant with namespace",
			route: &gwtypes.HTTPRoute{
				Namespace: "example-namespace",
				Name:      "example-name",
			},
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Kind:      new(gwtypes.Kind("example-kind")),
					Group:     new(gwtypes.Group("example-group")),
					Name:      "example-name",
					Namespace: new(gwtypes.Namespace("example-namespace")),
				},
			},
			allowedRefs: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				"example-namespace": {
					{
						Kind:  gwtypes.Kind("example-kind"),
						Group: gwtypes.Group("example-group"),
						Name:  new(gwtypes.ObjectName("example-name")),
					},
				},
			},
			expected: true,
		},
		{
			name: "allowed because backendRef and route use the same namespace",
			route: &gwtypes.HTTPRoute{
				Namespace: "example-namespace",
				Name:      "example-name",
			},
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Kind:      new(gwtypes.Kind("example-kind")),
					Group:     new(gwtypes.Group("example-group")),
					Name:      "example-name",
					Namespace: new(gwtypes.Namespace("example-namespace")),
				},
			},
			expected: true,
		},
		{
			name: "not allowed when not in grant and using different namespace",
			route: &gwtypes.HTTPRoute{
				Namespace: "example-namespace-2",
				Name:      "example-name",
			},
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Kind:      new(gwtypes.Kind("example-kind")),
					Group:     new(gwtypes.Group("example-group")),
					Name:      "example-name",
					Namespace: new(gwtypes.Namespace("example-namespace")),
				},
			},
			expected: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			rc := gatewayapi.NewRefCheckerForRoute(logr.Discard(), tc.route, tc.backendRef)
			result := rc.IsRefAllowedByGrant(tc.allowedRefs)
			require.Equal(t, tc.expected, result)
		})
	}
}
