package namegen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewName(t *testing.T) {
	tests := []struct {
		name        string
		httpRouteID string
		parentRefID string
		sectionID   string
		expected    *Name
	}{
		{
			name:        "all parameters provided",
			httpRouteID: "test-ns-test-route",
			parentRefID: "cp123456",
			sectionID:   "res789",
			expected: &Name{
				httpRouteID:    "test-ns-test-route",
				controlPlaneID: "cp123456",
				sectionID:      "res789",
			},
		},
		{
			name:        "empty section ID",
			httpRouteID: "test-ns-test-route",
			parentRefID: "cp123456",
			sectionID:   "",
			expected: &Name{
				httpRouteID:    "test-ns-test-route",
				controlPlaneID: "cp123456",
				sectionID:      "",
			},
		},
		{
			name:        "all empty strings",
			httpRouteID: "",
			parentRefID: "",
			sectionID:   "",
			expected: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewName(tt.httpRouteID, tt.parentRefID, tt.sectionID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestName_String(t *testing.T) {
	tests := []struct {
		name        string
		nameObj     *Name
		expected    string
		description string
	}{
		{
			name: "short name with section",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "cp123",
				sectionID:      "res456",
			},
			expected:    "test-ns-route.cp123.res456",
			description: "should join all parts with dots when length is acceptable",
		},
		{
			name: "short name without http route",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "cp123",
				sectionID:      "res456",
			},
			expected:    "cp123.res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name without parent reference",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "",
				sectionID:      "res456",
			},
			expected:    "test-ns-route.res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name without section",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "cp123",
				sectionID:      "",
			},
			expected:    "test-ns-route.cp123",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just httproute",
			nameObj: &Name{
				httpRouteID:    "test-ns-route",
				controlPlaneID: "",
				sectionID:      "",
			},
			expected:    "test-ns-route",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just parentref",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "cp123",
				sectionID:      "",
			},
			expected:    "cp123",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "short name with just section",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "res456",
			},
			expected:    "res456",
			description: "should join only non-empty parts with dots",
		},
		{
			name: "empty name",
			nameObj: &Name{
				httpRouteID:    "",
				controlPlaneID: "",
				sectionID:      "",
			},
			expected:    "",
			description: "should handle empty strings gracefully",
		},
		{
			name: "very long names that need hashing",
			nameObj: &Name{
				httpRouteID:    strings.Repeat("a", 100),
				controlPlaneID: strings.Repeat("b", 100),
				sectionID:      strings.Repeat("c", 100),
			},
			expected:    "", // Will be set dynamically in test
			description: "should hash long components and stay within length limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.nameObj.String()

			if tt.name == "very long names that need hashing" {
				// For long names, verify the result is within limits and has expected prefixes
				assert.LessOrEqual(t, len(result), 253-len("faf385ae")-1, "result should be within max length")
				parts := strings.Split(result, ".")
				require.Len(t, parts, 3, "should have 3 parts after hashing")
				assert.True(t, strings.HasPrefix(parts[0], "http"), "first part should have 'http' prefix")
				assert.True(t, strings.HasPrefix(parts[1], "cp"), "second part should have 'cp' prefix")
				assert.True(t, strings.HasPrefix(parts[2], "res"), "third part should have 'res' prefix")
			} else {
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestName_String_Hashing(t *testing.T) {
	// Create names that will definitely trigger hashing
	longHTTPRoute := strings.Repeat("httproute", 50)
	longParentRef := strings.Repeat("parentref", 50)
	longSection := strings.Repeat("section", 50)

	nameObj := NewName(longHTTPRoute, longParentRef, longSection)
	result := nameObj.String()

	// Should be hashed and within limits
	maxLen := 253 - len("faf385ae") - 1
	assert.LessOrEqual(t, len(result), maxLen)

	// Should contain the expected prefixes for hashed components
	parts := strings.Split(result, ".")
	assert.Len(t, parts, 3)
	assert.True(t, strings.HasPrefix(parts[0], "http"))
	assert.True(t, strings.HasPrefix(parts[1], "cp"))
	assert.True(t, strings.HasPrefix(parts[2], "res"))

	// Each hashed part should be reasonably short
	for _, part := range parts {
		assert.LessOrEqual(t, len(part), 50, "hashed parts should be reasonably short")
	}
}

func TestName_String_Consistency(t *testing.T) {
	tests := []struct {
		name        string
		httpRouteID string
		parentRefID string
		sectionID   string
	}{
		{
			name:        "normal components",
			httpRouteID: "test-route",
			parentRefID: "cp123",
			sectionID:   "res456",
		},
		{
			name:        "long components (trigger hashing)",
			httpRouteID: strings.Repeat("httproute", 50),
			parentRefID: strings.Repeat("parentref", 50),
			sectionID:   strings.Repeat("section", 50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nameObj1 := NewName(tt.httpRouteID, tt.parentRefID, tt.sectionID)
			nameObj2 := NewName(tt.httpRouteID, tt.parentRefID, tt.sectionID)

			result1 := nameObj1.String()
			result2 := nameObj2.String()

			assert.Equal(t, result1, result2, "same inputs should produce same outputs")
		})
	}
}
