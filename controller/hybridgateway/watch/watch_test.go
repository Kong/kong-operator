package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// fakeClient is a minimal stub for client.Client, not used for actual List calls here.
type fakeClient struct{ client.Client }

func TestWatches(t *testing.T) {
	cl := &fakeClient{}
	tests := []struct {
		name     string
		obj      client.Object
		wantLen  int
		wantType []any
	}{
		{
			name:    "HTTPRoute",
			obj:     &gwtypes.HTTPRoute{},
			wantLen: 2,
			wantType: []any{
				&gwtypes.Gateway{},
				&gwtypes.GatewayClass{},
			},
		},
		{
			name:    "OtherType",
			obj:     &gwtypes.Gateway{},
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
