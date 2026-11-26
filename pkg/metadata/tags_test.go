package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
)

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name     string
		obj      ObjectWithAnnotations
		expected []string
	}{
		{
			name: "Single tag",
			obj: &configurationv1.KongConsumer{
				Annotations: map[string]string{
					AnnotationKeyTags: "tag1",
				},
			},
			expected: []string{"tag1"},
		},
		{
			name: "Multiple tags",
			obj: &configurationv1.KongConsumer{
				Annotations: map[string]string{
					AnnotationKeyTags: "tag1,tag2,tag3,tag-dummy-5",
				},
			},
			expected: []string{"tag1", "tag2", "tag3", "tag-dummy-5"},
		},
		{
			name: "No tags",
			obj: &configurationv1.KongConsumer{
				Annotations: map[string]string{
					"other-annotation": "value",
				},
			},
			expected: nil,
		},
		{
			name:     "Nil object",
			obj:      nil,
			expected: nil,
		},
		{
			name:     "Nil pointer object",
			obj:      (*configurationv1.KongConsumer)(nil),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTags(tt.obj)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestReservedTags(t *testing.T) {
	tests := []struct {
		name             string
		tags             []string
		expectedReserved []string
		expectedFiltered []string
	}{
		{
			name: "nil tags",
		},
		{
			name:             "no reserved tags",
			tags:             []string{"tag1", "k8s-team:foo", "k8s-uid", "uid:k8s-uid:1234"},
			expectedFiltered: []string{"tag1", "k8s-team:foo", "k8s-uid", "uid:k8s-uid:1234"},
		},
		{
			name: "all reserved prefixes",
			tags: []string{
				"k8s-generation:1",
				"k8s-group:configuration.konghq.com",
				"k8s-kind:KongService",
				"k8s-name:svc",
				"k8s-namespace:default",
				"k8s-uid:1234",
				"k8s-version:v1alpha1",
				"managed-by:kong-operator",
			},
			expectedReserved: []string{
				"k8s-generation:1",
				"k8s-group:configuration.konghq.com",
				"k8s-kind:KongService",
				"k8s-name:svc",
				"k8s-namespace:default",
				"k8s-uid:1234",
				"k8s-version:v1alpha1",
				"managed-by:kong-operator",
			},
		},
		{
			name:             "mixed tags with surrounding whitespace",
			tags:             []string{"tag1", " k8s-uid:1234", "tag2", "managed-by:"},
			expectedReserved: []string{" k8s-uid:1234", "managed-by:"},
			expectedFiltered: []string{"tag1", "tag2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expectedReserved, ReservedTags(tt.tags))
			require.Equal(t, tt.expectedFiltered, WithoutReservedTags(tt.tags))
		})
	}
}
