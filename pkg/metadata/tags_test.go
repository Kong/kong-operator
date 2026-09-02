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
