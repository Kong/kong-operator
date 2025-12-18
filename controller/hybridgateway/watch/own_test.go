package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestOwns(t *testing.T) {
	tests := []struct {
		name    string
		obj     client.Object
		wantLen int
		want    []any
	}{
		{
			name:    "HTTPRoute",
			obj:     &gwtypes.HTTPRoute{},
			wantLen: 6,
			want: []any{
				&configurationv1alpha1.KongRoute{},
				&configurationv1alpha1.KongService{},
				&configurationv1alpha1.KongUpstream{},
				&configurationv1alpha1.KongTarget{},
				&configurationv1alpha1.KongPluginBinding{},
				&configurationv1.KongPlugin{},
			},
		},
		{
			name:    "Gateway",
			obj:     &gwtypes.Gateway{},
			wantLen: 2,
			want: []any{
				&configurationv1alpha1.KongCertificate{},
				&configurationv1alpha1.KongSNI{},
			},
		},
		{
			name:    "OtherType",
			obj:     &gwtypes.GatewayClass{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owned := Owns(tt.obj)
			if tt.wantLen == 0 {
				require.Nil(t, owned)
			} else {
				require.Len(t, owned, tt.wantLen)
				for i, want := range tt.want {
					require.IsType(t, want, owned[i])
				}
			}
		})
	}
}
