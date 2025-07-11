package extensions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

func TestMergeExtensions(t *testing.T) {
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
			result := MergeExtensions(tt.defaultExtensions, tt.extensions)
			assert.Equal(t, tt.expected, result)
		})
	}
}
