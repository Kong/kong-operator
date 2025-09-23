package intermediate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestName_String(t *testing.T) {
	tests := []struct {
		name     string
		nameObj  Name
		expected string
	}{
		{
			name: "basic name without indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{},
			},
			expected: "prefix.namespace.name",
		},
		{
			name: "name with single index",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1},
			},
			expected: "prefix.namespace.name.1",
		},
		{
			name: "name with multiple indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2, 3},
			},
			expected: "prefix.namespace.name.1.2.3",
		},
		{
			name: "name with zero indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{0, 0, 0},
			},
			expected: "prefix.namespace.name.0.0.0",
		},
		{
			name: "empty strings",
			nameObj: Name{
				prefix:    "",
				namespace: "",
				name:      "",
				indexes:   []int{},
			},
			expected: "..",
		},
		{
			name: "short name under max length",
			nameObj: Name{
				prefix:    "short",
				namespace: "ns",
				name:      "nm",
				indexes:   []int{1},
			},
			expected: "short.ns.nm.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_String_Truncation(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     Name
		description string
	}{
		{
			name: "long name requiring truncation",
			nameObj: Name{
				prefix:    "very-long-prefix-that-takes-up-space",
				namespace: strings.Repeat("a", 100),
				name:      strings.Repeat("b", 100),
				indexes:   []int{1, 2, 3},
			},
			description: "should truncate namespace and name proportionally",
		},
		{
			name: "extremely long namespace and name",
			nameObj: Name{
				prefix:    "prefix",
				namespace: strings.Repeat("namespace", 20),
				name:      strings.Repeat("name", 20),
				indexes:   []int{10, 20, 30},
			},
			description: "should handle moderately long inputs",
		},
		{
			name: "long prefix with short namespace and name",
			nameObj: Name{
				prefix:    strings.Repeat("prefix", 25),
				namespace: "ns",
				name:      "name",
				indexes:   []int{1},
			},
			description: "should handle long prefix",
		},
		{
			name: "large indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: strings.Repeat("a", 50),
				name:      strings.Repeat("b", 50),
				indexes:   []int{99999, 88888, 77777},
			},
			description: "should handle large index values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.String()
			assert.LessOrEqual(t, len(result), 260, "Result should be within reasonable bounds")
			if tt.nameObj.prefix != "" {
				assert.Contains(t, result, tt.nameObj.prefix, "Should contain the prefix")
			}
			parts := strings.Split(result, ".")
			assert.GreaterOrEqual(t, len(parts), 3, "Should have at least prefix, namespace, and name parts")
		})
	}
}

func TestName_String_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     Name
		description string
	}{
		{
			name: "moderate length calculation",
			nameObj: Name{
				prefix:    "prefix",
				namespace: strings.Repeat("a", 80),
				name:      strings.Repeat("b", 80),
				indexes:   []int{1},
			},
			description: "should handle names requiring truncation",
		},
		{
			name: "nil indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   nil,
			},
			description: "should handle nil indexes",
		},
		{
			name: "negative indexes",
			nameObj: Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{-1, -2, -3},
			},
			description: "should handle negative indexes",
		},
		{
			name: "extremely long prefix consuming most space",
			nameObj: Name{
				prefix:    strings.Repeat("x", 240), // Very long prefix
				namespace: strings.Repeat("a", 50),
				name:      strings.Repeat("b", 50),
				indexes:   []int{12345},
			},
			description: "should handle cases where prefix consumes most space",
		},
		{
			name: "edge case with very large indexes",
			nameObj: Name{
				prefix:    strings.Repeat("prefix", 20),  // 120 chars
				namespace: strings.Repeat("ns", 50),      // 100 chars
				name:      strings.Repeat("nm", 50),      // 100 chars
				indexes:   []int{999999, 888888, 777777}, // Large indexes
			},
			description: "should handle large indexes that affect reserved space calculation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.String()
			assert.LessOrEqual(t, len(result), 260, "Should respect reasonable max length")
			assert.NotEmpty(t, result, "Result should not be empty")
		})
	}
}

func TestName_GetParentRefIndex(t *testing.T) {
	tests := []struct {
		name     string
		nameObj  *Name
		expected int
	}{
		{
			name: "valid parent ref index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{5, 2, 3},
			},
			expected: 5,
		},
		{
			name: "zero parent ref index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{0, 2, 3},
			},
			expected: 0,
		},
		{
			name: "negative parent ref index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{-1, 2, 3},
			},
			expected: -1,
		},
		{
			name: "empty indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{},
			},
			expected: -1,
		},
		{
			name: "nil indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   nil,
			},
			expected: -1,
		},
		{
			name:     "nil name object",
			nameObj:  nil,
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.GetParentRefIndex()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_GetRuleIndex(t *testing.T) {
	tests := []struct {
		name     string
		nameObj  *Name
		expected int
	}{
		{
			name: "valid rule index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 7, 3},
			},
			expected: 7,
		},
		{
			name: "zero rule index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 0, 3},
			},
			expected: 0,
		},
		{
			name: "negative rule index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, -5, 3},
			},
			expected: -5,
		},
		{
			name: "only one index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1},
			},
			expected: -1,
		},
		{
			name: "empty indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{},
			},
			expected: -1,
		},
		{
			name: "nil indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   nil,
			},
			expected: -1,
		},
		{
			name:     "nil name object",
			nameObj:  nil,
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.GetRuleIndex()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_GetMatchIndex(t *testing.T) {
	tests := []struct {
		name     string
		nameObj  *Name
		expected int
	}{
		{
			name: "valid match index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2, 9},
			},
			expected: 9,
		},
		{
			name: "zero match index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2, 0},
			},
			expected: 0,
		},
		{
			name: "negative match index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2, -10},
			},
			expected: -10,
		},
		{
			name: "only two indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2},
			},
			expected: -1,
		},
		{
			name: "only one index",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1},
			},
			expected: -1,
		},
		{
			name: "empty indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{},
			},
			expected: -1,
		},
		{
			name: "nil indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   nil,
			},
			expected: -1,
		},
		{
			name:     "nil name object",
			nameObj:  nil,
			expected: -1,
		},
		{
			name: "more than three indexes",
			nameObj: &Name{
				prefix:    "prefix",
				namespace: "namespace",
				name:      "name",
				indexes:   []int{1, 2, 3, 4, 5},
			},
			expected: 3, // Should return the third index (index 2)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.GetMatchIndex()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_ComprehensiveScenarios(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     Name
		description string
	}{
		{
			name: "real world scenario - kong route",
			nameObj: Name{
				prefix:    "kong-route",
				namespace: "default",
				name:      "api-gateway-route",
				indexes:   []int{0, 1, 2},
			},
			description: "typical Kong route naming scenario",
		},
		{
			name: "long namespace scenario",
			nameObj: Name{
				prefix:    "kong-service",
				namespace: "very-long-namespace-with-many-characters",
				name:      "short-service",
				indexes:   []int{10, 20},
			},
			description: "scenario with long namespace requiring truncation",
		},
		{
			name: "unicode characters",
			nameObj: Name{
				prefix:    "kong-测试",
				namespace: "命名空间",
				name:      "服务名称",
				indexes:   []int{1},
			},
			description: "unicode characters in names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test String method
			result := tt.nameObj.String()
			assert.NotEmpty(t, result, "String result should not be empty")
			assert.LessOrEqual(t, len(result), 260, "Should respect reasonable max length")

			// Test index getters
			if len(tt.nameObj.indexes) >= 1 {
				assert.Equal(t, tt.nameObj.indexes[0], tt.nameObj.GetParentRefIndex())
			} else {
				assert.Equal(t, -1, tt.nameObj.GetParentRefIndex())
			}

			if len(tt.nameObj.indexes) >= 2 {
				assert.Equal(t, tt.nameObj.indexes[1], tt.nameObj.GetRuleIndex())
			} else {
				assert.Equal(t, -1, tt.nameObj.GetRuleIndex())
			}

			if len(tt.nameObj.indexes) >= 3 {
				assert.Equal(t, tt.nameObj.indexes[2], tt.nameObj.GetMatchIndex())
			} else {
				assert.Equal(t, -1, tt.nameObj.GetMatchIndex())
			}
		})
	}
}
