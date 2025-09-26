package managedfields

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPruneEmptyFields_AllBranches(t *testing.T) {
	cases := []struct {
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name:   "removes empty map",
			input:  map[string]any{"a": map[string]any{}},
			expect: map[string]any{},
		},
		{
			name:   "removes empty slice",
			input:  map[string]any{"a": []any{}},
			expect: map[string]any{},
		},
		{
			name:   "removes zero value",
			input:  map[string]any{"a": "", "b": nil},
			expect: map[string]any{},
		},
		{
			name:   "keeps non-empty map",
			input:  map[string]any{"a": map[string]any{"b": 1}},
			expect: map[string]any{"a": map[string]any{"b": 1}},
		},
		{
			name:   "keeps non-empty slice",
			input:  map[string]any{"a": []any{1, 2}},
			expect: map[string]any{"a": []any{1, 2}},
		},
		{
			name:   "recursively prunes nested empty map",
			input:  map[string]any{"a": map[string]any{"b": map[string]any{}}},
			expect: map[string]any{"a": map[string]any{}},
		},
		{
			name:   "recursively prunes nested empty slice",
			input:  map[string]any{"a": []any{map[string]any{}, 1}},
			expect: map[string]any{"a": []any{map[string]any{}, 1}},
		},
	}

	for _, tc := range cases {
		u := &unstructured.Unstructured{Object: tc.input}
		PruneEmptyFields(u)
		assert.Equal(t, tc.expect, u.Object, tc.name)
	}
}
