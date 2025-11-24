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
			expect: map[string]any{},
		},
		{
			name: "removes map that becomes empty after pruning",
			input: map[string]any{
				"outer": map[string]any{
					"inner": map[string]any{
						"empty": map[string]any{},
						"zero":  "",
					},
				},
			},
			expect: map[string]any{},
		},
		{
			name: "removes empty maps from slice",
			input: map[string]any{
				"items": []any{
					map[string]any{},
					map[string]any{"keep": "value"},
					map[string]any{"remove": ""},
				},
			},
			expect: map[string]any{
				"items": []any{
					map[string]any{"keep": "value"},
				},
			},
		},
		{
			name: "removes slice that becomes empty after removing empty maps",
			input: map[string]any{
				"items": []any{
					map[string]any{},
					map[string]any{"zero": ""},
				},
			},
			expect: map[string]any{},
		},
		{
			name: "handles mixed slice with empty maps and other types",
			input: map[string]any{
				"mixed": []any{
					map[string]any{}, // should be removed
					"string",         // should be kept
					map[string]any{"nested": map[string]any{}}, // should be removed (becomes empty)
					42, // should be kept
				},
			},
			expect: map[string]any{
				"mixed": []any{"string", 42},
			},
		},
		{
			name: "deeply nested pruning with maps and slices",
			input: map[string]any{
				"level1": map[string]any{
					"level2": []any{
						map[string]any{
							"level3": map[string]any{
								"empty": map[string]any{},
							},
						},
						map[string]any{
							"keep": "value",
						},
					},
				},
			},
			expect: map[string]any{
				"level1": map[string]any{
					"level2": []any{
						map[string]any{
							"keep": "value",
						},
					},
				},
			},
		},
		{
			name: "preserves non-map elements in slice",
			input: map[string]any{
				"items": []any{
					"string1",
					map[string]any{}, // empty map should be removed
					123,
					map[string]any{"key": "value"}, // non-empty map should be kept
					true,
				},
			},
			expect: map[string]any{
				"items": []any{
					"string1",
					123,
					map[string]any{"key": "value"},
					true,
				},
			},
		},
		{
			name:   "recursively prunes nested empty slice",
			input:  map[string]any{"a": []any{map[string]any{}, 1}},
			expect: map[string]any{"a": []any{1}},
		},
		{
			name:   "keeps false boolean value",
			input:  map[string]any{"enabled": false, "disabled": false},
			expect: map[string]any{"enabled": false, "disabled": false},
		},
		{
			name:   "keeps true boolean value",
			input:  map[string]any{"enabled": true},
			expect: map[string]any{"enabled": true},
		},
		{
			name:   "keeps pointer to zero integer",
			input:  map[string]any{"retries": new(int)}, // pointer to 0
			expect: map[string]any{"retries": new(int)},
		},
		{
			name: "removes nil pointer",
			input: map[string]any{
				"timeout": (*int)(nil),
			},
			expect: map[string]any{},
		},
		{
			name: "keeps pointer to zero int64",
			input: func() map[string]any {
				val := int64(0)
				return map[string]any{"timeout": &val}
			}(),
			expect: func() map[string]any {
				val := int64(0)
				return map[string]any{"timeout": &val}
			}(),
		},
		{
			name: "keeps pointer to non-zero value",
			input: func() map[string]any {
				val := int64(5)
				return map[string]any{"timeout": &val}
			}(),
			expect: func() map[string]any {
				val := int64(5)
				return map[string]any{"timeout": &val}
			}(),
		},
		{
			name: "mixed booleans and pointers",
			input: func() map[string]any {
				retries := 0
				timeout := int64(100)
				return map[string]any{
					"enabled":  false,
					"retries":  &retries,
					"timeout":  &timeout,
					"nilPtr":   (*int)(nil),
					"emptyStr": "",
					"keepThis": "value",
				}
			}(),
			expect: func() map[string]any {
				retries := 0
				timeout := int64(100)
				return map[string]any{
					"enabled":  false,
					"retries":  &retries,
					"timeout":  &timeout,
					"keepThis": "value",
				}
			}(),
		},
		{
			name: "nested structure with booleans and pointers",
			input: func() map[string]any {
				zero := 0
				return map[string]any{
					"config": map[string]any{
						"stripPath": false,
						"retries":   &zero,
						"emptyMap":  map[string]any{},
						"name":      "",
					},
				}
			}(),
			expect: func() map[string]any {
				zero := 0
				return map[string]any{
					"config": map[string]any{
						"stripPath": false,
						"retries":   &zero,
					},
				}
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: tc.input}
			PruneEmptyFields(u)
			assert.Equal(t, tc.expect, u.Object, tc.name)
		})
	}
}
