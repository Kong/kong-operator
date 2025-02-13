package v1alpha1_test

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

func TestControlPlaneRefStringer(t *testing.T) {
	testCases := []struct {
		name     string
		ref      *commonv1alpha1.ControlPlaneRef
		expected string
	}{
		{
			name: "unknown type - doesn't panic",
			ref: &commonv1alpha1.ControlPlaneRef{
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
			ref: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "foo",
				},
			},
			expected: "<konnectNamespacedRef:foo>",
		},
		{
			name: "konnectNamespacedRef with namespace",
			ref: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Namespace: "bar",
					Name:      "foo",
				},
			},
			expected: "<konnectNamespacedRef:bar/foo>",
		},
		{
			name: "konnectID without ID - doesn't panic",
			ref: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectID,
			},
			expected: "<konnectID:nil>",
		},
		{
			name: "konnectID with ID",
			ref: &commonv1alpha1.ControlPlaneRef{
				Type:      commonv1alpha1.ControlPlaneRefKonnectID,
				KonnectID: lo.ToPtr("foo"),
			},
			expected: "<konnectID:foo>",
		},
		{
			name: "kic",
			ref: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKIC,
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
