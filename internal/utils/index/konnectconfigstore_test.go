package index

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestKonnectConfigStoreOnControlPlaneRef(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		ref  string
		want []string
	}{
		{
			name: "konnectNamespacedRef defaults to the store namespace",
			ref:  `{"type":"konnectNamespacedRef","konnectNamespacedRef":{"name":"cp"}}`,
			want: []string{"default/cp"},
		},
		{
			name: "konnectNamespacedRef uses the explicit namespace",
			ref:  `{"type":"konnectNamespacedRef","konnectNamespacedRef":{"name":"cp","namespace":"other"}}`,
			want: []string{"other/cp"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &konnectv1alpha1.KonnectConfigStore{Namespace: "default"}
			require.NoError(t, json.Unmarshal([]byte(tt.ref), &store.Spec.ControlPlaneRef))
			require.Equal(t, tt.want, konnectConfigStoreOnKonnectGatewayControlPlaneRef(store))
		})
	}
}
