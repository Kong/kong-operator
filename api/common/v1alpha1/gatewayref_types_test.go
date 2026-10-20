package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestGatewayRef(t *testing.T) {
	ref := commonv1alpha1.GatewayRef{
		Group:     "eventgateway.konghq.com",
		Kind:      "OnPremEventGateway",
		Name:      "gw",
		Namespace: new("other"),
	}

	t.Run("IsGroupKind", func(t *testing.T) {
		require.True(t, ref.IsGroupKind("eventgateway.konghq.com", "OnPremEventGateway"))
		require.False(t, ref.IsGroupKind("konnect.konghq.com", "OnPremEventGateway"))
		require.False(t, ref.IsGroupKind("eventgateway.konghq.com", "KonnectEventGateway"))
	})

	t.Run("ToObjectRef", func(t *testing.T) {
		require.Equal(t,
			commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw", Namespace: new("other")},
			},
			ref.ToObjectRef(),
		)

		ref := commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: "gw"}
		require.Equal(t,
			commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			ref.ToObjectRef(),
		)
	})
}
