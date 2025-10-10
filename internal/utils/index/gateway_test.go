package index

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestOptionsForGateway(t *testing.T) {
	options := OptionsForGateway()
	require.Len(t, options, 1)
	opt := options[0]
	require.IsType(t, &gwtypes.Gateway{}, opt.Object)
	require.Equal(t, GatewayClassOnGatewayIndex, opt.Field)
	require.NotNil(t, opt.ExtractValueFn)
}

func TestGatewayClassOnGateway(t *testing.T) {
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "valid GatewayClassName",
			obj: &gwtypes.Gateway{
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "my-class",
				},
			},
			want: []string{"my-class"},
		},
		{
			name: "empty GatewayClassName",
			obj: &gwtypes.Gateway{
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "",
				},
			},
			want: nil,
		},
		{
			name: "wrong type",
			obj:  &gwtypes.HTTPRoute{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GatewayClassOnGateway(tt.obj)
			require.Equal(t, tt.want, got)
		})
	}
}
