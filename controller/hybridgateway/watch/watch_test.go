package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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
			name:    "HTTPRoute with ReferenceGrant enabled",
			obj:     &gwtypes.HTTPRoute{},
			wantLen: 11,
			wantType: []any{
				&gwtypes.Gateway{},
				&gwtypes.GatewayClass{},
				&corev1.Service{},
				&discoveryv1.EndpointSlice{},
				&configurationv1alpha1.KongUpstream{},
				&configurationv1alpha1.KongTarget{},
				&configurationv1alpha1.KongService{},
				&configurationv1alpha1.KongRoute{},
				&configurationv1.KongPlugin{},
				&configurationv1alpha1.KongPluginBinding{},
				&gwtypes.ReferenceGrant{},
			},
		},
		{
			name:    "Gateway",
			obj:     &gwtypes.Gateway{},
			wantLen: 2,
			wantType: []any{
				&corev1.Secret{},
				&gwtypes.ReferenceGrant{},
			},
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
