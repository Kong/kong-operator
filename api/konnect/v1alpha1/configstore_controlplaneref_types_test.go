package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestConfigStoreControlPlaneRefFormats(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		json string
		want commonv1alpha1.ObjectRef
	}{
		{
			name: "konnectNamespacedRef in the same namespace",
			json: `{"type":"konnectNamespacedRef","konnectNamespacedRef":{"name":"cp"}}`,
			want: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "cp"},
			},
		},
		{
			name: "konnectNamespacedRef with an explicit namespace",
			json: `{"type":"konnectNamespacedRef","konnectNamespacedRef":{"name":"cp","namespace":"other"}}`,
			want: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "cp", Namespace: new("other")},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var store KonnectConfigStore
			require.NoError(t, json.Unmarshal([]byte(tt.json), &store.Spec.ControlPlaneRef))
			require.Equal(t, tt.want, store.GetParentRef())
			require.Equal(t, tt.want, store.GetControlPlaneRef())

			store.SetParentRef(tt.want)
			require.Equal(t, tt.want, store.GetParentRef())
			encoded, err := json.Marshal(store.Spec.ControlPlaneRef)
			require.NoError(t, err)
			require.JSONEq(t, tt.json, string(encoded))
		})
	}
}
