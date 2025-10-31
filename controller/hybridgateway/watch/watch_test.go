package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestWatches(t *testing.T) {
	cl := fake.NewClientBuilder().Build()
	tests := []struct {
		name     string
		obj      client.Object
		wantLen  int
		wantType []any
	}{
		{
			name:    "HTTPRoute",
			obj:     &gwtypes.HTTPRoute{},
			wantLen: 4,
			wantType: []any{
				&gwtypes.Gateway{},
				&gwtypes.GatewayClass{},
				&corev1.Service{},
				&discoveryv1.EndpointSlice{},
			},
		},
		{
			name:    "Gateway",
			obj:     &gwtypes.Gateway{},
			wantLen: 0,
		},
		{
			name:    "GatewayClass",
			obj:     &gwtypes.GatewayClass{},
			wantLen: 0,
		},
		{
			name:    "Service",
			obj:     &corev1.Service{},
			wantLen: 0,
		},
		{
			name:    "EndpointSlice",
			obj:     &discoveryv1.EndpointSlice{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watchers := Watches(tt.obj, cl)
			if tt.wantLen == 0 {
				require.Nil(t, watchers)
			} else {
				require.Len(t, watchers, tt.wantLen)
				for i, want := range tt.wantType {
					require.IsType(t, want, watchers[i].Object)
				}
			}
		})
	}
}
