package intermediate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashName_String(t *testing.T) {
	tests := []struct {
		name     string
		hashName HashName
		expected string
	}{
		{
			name: "basic hash name",
			hashName: HashName{
				prefix:    "prefix",
				namespace: "namespace",
				hash:      "abc123",
			},
			expected: "prefix.namespace.abc123",
		},
		{
			name: "all fields empty",
			hashName: HashName{
				prefix:    "",
				namespace: "",
				hash:      "",
			},
			expected: "..",
		},
		{
			name: "exactly at 253 character limit",
			hashName: HashName{
				prefix:    strings.Repeat("a", 100),
				namespace: strings.Repeat("b", 100),
				hash:      strings.Repeat("c", 51), // 100 + 1 + 100 + 1 + 51 = 253
			},
			expected: strings.Repeat("a", 100) + "." + strings.Repeat("b", 100) + "." + strings.Repeat("c", 51),
		},
		{
			name: "truncation needed - long prefix and namespace",
			hashName: HashName{
				prefix:    strings.Repeat("prefix", 50),    // 300 chars
				namespace: strings.Repeat("namespace", 30), // 270 chars
				hash:      "hash123",                       // 7 chars
			},
			// With hash = 7 chars, reserved = 9 (7 + 2 dots)
			// Remaining = 253 - 9 = 244
			// prefixMax = 244 / 2 = 122
			// namespaceMax = 244 - 122 = 122
			expected: strings.Repeat("prefix", 50)[:122] + "." + strings.Repeat("namespace", 30)[:122] + ".hash123",
		},
		{
			name: "truncation needed - very long prefix",
			hashName: HashName{
				prefix:    strings.Repeat("verylongprefix", 30), // 420 chars
				namespace: "short",
				hash:      "hash456",
			},
			// With hash = 7 chars, reserved = 9
			// Remaining = 244, prefixMax = 122, namespaceMax = 122
			expected: strings.Repeat("verylongprefix", 30)[:122] + ".short.hash456",
		},
		{
			name: "truncation needed - very long namespace",
			hashName: HashName{
				prefix:    "short",
				namespace: strings.Repeat("verylongnamespace", 25), // 425 chars
				hash:      "hash789",
			},
			// With hash = 7 chars, reserved = 9
			// Remaining = 244, prefixMax = 122, namespaceMax = 122
			expected: "short." + strings.Repeat("verylongnamespace", 25)[:122] + ".hash789",
		},
		{
			name: "very long hash",
			hashName: HashName{
				prefix:    "prefix",
				namespace: "namespace",
				hash:      strings.Repeat("h", 200), // 200 chars
			},
			// With hash = 200 chars, reserved = 201
			// Remaining = 52, prefixMax = 26, namespaceMax = 26
			// Since "prefix" (6 chars) < 26 and "namespace" (9 chars) < 26, they won't be truncated
			expected: "prefix" + "." + "namespace" + "." + strings.Repeat("h", 200),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.hashName.String()
			assert.Equal(t, tt.expected, result)

			// Verify the result doesn't exceed 253 characters
			assert.LessOrEqual(t, len(result), 253, "Result should not exceed 253 characters")

			// Verify the result always contains exactly 2 dots
			dotCount := strings.Count(result, ".")
			assert.Equal(t, 2, dotCount, "Result should contain exactly 2 dots")

			// Verify the hash is always preserved at the end
			parts := strings.Split(result, ".")
			assert.Equal(t, tt.hashName.hash, parts[2], "Hash should be preserved")
		})
	}
}

func TestHashName_GetHash(t *testing.T) {
	tests := []struct {
		name     string
		hashName HashName
		expected string
	}{
		{
			name: "basic hash",
			hashName: HashName{
				prefix:    "prefix",
				namespace: "namespace",
				hash:      "abc123",
			},
			expected: "abc123",
		},
		{
			name: "empty hash",
			hashName: HashName{
				prefix:    "prefix",
				namespace: "namespace",
				hash:      "",
			},
			expected: "",
		},
		{
			name: "long hash",
			hashName: HashName{
				prefix:    "prefix",
				namespace: "namespace",
				hash:      strings.Repeat("abcdef", 50),
			},
			expected: strings.Repeat("abcdef", 50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.hashName.GetHash()
			assert.Equal(t, tt.expected, result)
		})
	}
}
