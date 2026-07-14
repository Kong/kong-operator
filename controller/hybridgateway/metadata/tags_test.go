package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{name: "nil annotations", annotations: nil, expected: nil},
		{name: "absent annotation", annotations: map[string]string{"other": "x"}, expected: nil},
		{name: "empty value", annotations: map[string]string{"konghq.com/tags": ""}, expected: nil},
		{name: "single", annotations: map[string]string{"konghq.com/tags": "foo"}, expected: []string{"foo"}},
		{name: "multiple", annotations: map[string]string{"konghq.com/tags": "foo,bar"}, expected: []string{"foo", "bar"}},
		{name: "whitespace trimmed", annotations: map[string]string{"konghq.com/tags": " foo , bar "}, expected: []string{"foo", "bar"}},
		{name: "trailing comma dropped", annotations: map[string]string{"konghq.com/tags": "foo, bar, "}, expected: []string{"foo", "bar"}},
		{name: "all empty entries", annotations: map[string]string{"konghq.com/tags": " , , "}, expected: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ExtractTags(tc.annotations))
		})
	}
}
