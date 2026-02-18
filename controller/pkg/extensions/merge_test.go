package extensions

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestMergeExtensionsForType(t *testing.T) {
	tests := []struct {
		name              string
		defaultExtensions []commonv1alpha1.ExtensionRef
		extensions        []commonv1alpha1.ExtensionRef
		expected          []commonv1alpha1.ExtensionRef
	}{
		{
			name: "no overlap",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
			extensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group3",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name3",
					},
				},
			},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
				{
					Group: "group3",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name3",
					},
				},
			},
		},
		{
			name: "with overlap",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
			extensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group3",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name3",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "nameB",
					},
				},
			},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group3",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name3",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "nameB",
					},
				},
			},
		},
		{
			name: "only default extensions",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
			extensions: []commonv1alpha1.ExtensionRef{},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
		},
		{
			name:              "only user extensions",
			defaultExtensions: []commonv1alpha1.ExtensionRef{},
			extensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name2",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeExtensionsForDataPlane(tt.defaultExtensions, tt.extensions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockExtendable is a mock implementation of the extendable interface for testing
type mockExtendable struct {
	extensions []commonv1alpha1.ExtensionRef
}

func (m *mockExtendable) GetExtensions() []commonv1alpha1.ExtensionRef {
	return m.extensions
}

func TestMergeExtensions(t *testing.T) {
	tests := []struct {
		name              string
		defaultExtensions []commonv1alpha1.ExtensionRef
		extendable        interface {
			GetExtensions() []commonv1alpha1.ExtensionRef
		}
		expected []commonv1alpha1.ExtensionRef
	}{
		{
			name:              "both empty",
			defaultExtensions: []commonv1alpha1.ExtensionRef{},
			extendable:        &mockExtendable{extensions: []commonv1alpha1.ExtensionRef{}},
			expected:          nil,
		},
		{
			name: "nil user extensions",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
			},
			extendable: &mockExtendable{extensions: nil},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "name1",
					},
				},
			},
		},
		{
			name: "partial overlap with different names",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name:      "default-name",
						Namespace: lo.ToPtr("default-ns"),
					},
				},
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "unique-default",
					},
				},
			},
			extendable: &mockExtendable{
				extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "group1",
						Kind:  "kind1",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name:      "user-name",
							Namespace: lo.ToPtr("user-ns"),
						},
					},
					{
						Group: "group3",
						Kind:  "kind3",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "unique-user",
						},
					},
				},
			},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "group2",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "unique-default",
					},
				},
				{
					Group: "group1",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name:      "user-name",
						Namespace: lo.ToPtr("user-ns"),
					},
				},
				{
					Group: "group3",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "unique-user",
					},
				},
			},
		},
		{
			name: "same group different kind",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "common-group",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default1",
					},
				},
				{
					Group: "common-group",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default2",
					},
				},
			},
			extendable: &mockExtendable{
				extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "common-group",
						Kind:  "kind1",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "user1",
						},
					},
					{
						Group: "common-group",
						Kind:  "kind3",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "user3",
						},
					},
				},
			},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: "common-group",
					Kind:  "kind2",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default2",
					},
				},
				{
					Group: "common-group",
					Kind:  "kind1",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "user1",
					},
				},
				{
					Group: "common-group",
					Kind:  "kind3",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "user3",
					},
				},
			},
		},
		{
			name: "DataPlaneMetricsExtension is not added to DataPlane",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default1",
					},
				},
			},
			extendable: &operatorv1beta1.DataPlane{},
			expected:   nil,
		},
		{
			name: "DataPlaneMetricsExtension is added to ControlPlane",
			defaultExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default1",
					},
				},
			},
			extendable: &gwtypes.ControlPlane{},
			expected: []commonv1alpha1.ExtensionRef{
				{
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "default1",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeExtensions(tt.defaultExtensions, tt.extendable)
			assert.Equal(t, tt.expected, result)
		})
	}
}
