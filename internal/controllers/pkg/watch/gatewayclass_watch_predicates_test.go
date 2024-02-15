package watch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/pkg/vars"
)

func TestGatewayClassMatchesController(t *testing.T) {
	for _, tt := range []struct {
		name     string
		obj      client.Object
		expected bool
	}{
		{
			name: "controller_name_matches",
			obj: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
			},
			expected: true,
		},
		{
			name: "controller_name_does_not_match",
			obj: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "some-other-controller",
				},
			},
			expected: false,
		},
		{
			name:     "wrong_object_type",
			obj:      &gatewayv1.HTTPRoute{},
			expected: false,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := GatewayClassMatchesController(tt.obj)
			assert.Equal(t, tt.expected, result)
		})
	}
}
