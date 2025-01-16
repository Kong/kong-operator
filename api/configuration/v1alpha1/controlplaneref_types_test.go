package v1alpha1_test

import (
	"testing"

	"github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestControlPlaneRefStringer(t *testing.T) {
	testCases := []struct {
		name     string
		ref      *v1alpha1.ControlPlaneRef
		expected string
	}{
		{
			name: "unknown type - doesn't panic",
			ref: &v1alpha1.ControlPlaneRef{
				Type: "notSupportedType",
			},
			expected: "<unknown:notSupportedType>",
		},
		{
			name:     "nil - doesn't panic",
			ref:      nil,
			expected: "<nil>",
		},
		{
			name: "konnectNamespacedRef with no namespace",
			ref: &v1alpha1.ControlPlaneRef{
				Type: v1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &v1alpha1.KonnectNamespacedRef{
					Name: "foo",
				},
			},
			expected: "<konnectNamespacedRef:foo>",
		},
		{
			name: "konnectNamespacedRef with namespace",
			ref: &v1alpha1.ControlPlaneRef{
				Type: v1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &v1alpha1.KonnectNamespacedRef{
					Namespace: "bar",
					Name:      "foo",
				},
			},
			expected: "<konnectNamespacedRef:bar/foo>",
		},
		{
			name: "konnectID without ID - doesn't panic",
			ref: &v1alpha1.ControlPlaneRef{
				Type: v1alpha1.ControlPlaneRefKonnectID,
			},
			expected: "<konnectID:nil>",
		},
		{
			name: "konnectID with ID",
			ref: &v1alpha1.ControlPlaneRef{
				Type:      v1alpha1.ControlPlaneRefKonnectID,
				KonnectID: lo.ToPtr("foo"),
			},
			expected: "<konnectID:foo>",
		},
		{
			name: "kic",
			ref: &v1alpha1.ControlPlaneRef{
				Type: v1alpha1.ControlPlaneRefKIC,
			},
			expected: "<kic>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.ref.String())
		})
	}
}
