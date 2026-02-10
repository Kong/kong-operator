package generator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single sentence without period",
			input:    "This is a test",
			expected: []string{"This is a test"},
		},
		{
			name:     "single sentence with period",
			input:    "This is a test.",
			expected: []string{"This is a test."},
		},
		{
			name:     "two sentences",
			input:    "First sentence. Second sentence.",
			expected: []string{"First sentence.", "Second sentence."},
		},
		{
			name:     "three sentences",
			input:    "First. Second. Third.",
			expected: []string{"First.", "Second.", "Third."},
		},
		{
			name:     "sentence with period at end only",
			input:    "No splits here. ",
			expected: []string{"No splits here. "},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "period without space is not a boundary",
			input:    "file.txt is a filename",
			expected: []string{"file.txt is a filename"},
		},
		{
			name:     "multiple periods without spaces",
			input:    "v1.2.3 is a version",
			expected: []string{"v1.2.3 is a version"},
		},
		{
			name:  "real world example",
			input: "The default authentication strategy for APIs published to the portal. Newly published APIs will use this authentication strategy unless overridden during publication.",
			expected: []string{
				"The default authentication strategy for APIs published to the portal.",
				"Newly published APIs will use this authentication strategy unless overridden during publication.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitSentences(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWrapLongLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		expected []string
	}{
		{
			name:     "short line no wrap needed",
			input:    "Short line",
			maxWidth: 80,
			expected: []string{"Short line"},
		},
		{
			name:     "exact fit",
			input:    "Exact fit",
			maxWidth: 9,
			expected: []string{"Exact fit"},
		},
		{
			name:     "wrap at word boundary",
			input:    "This is a longer line that needs wrapping",
			maxWidth: 20,
			expected: []string{"This is a longer", "line that needs", "wrapping"},
		},
		{
			name:     "single long word",
			input:    "Supercalifragilisticexpialidocious",
			maxWidth: 10,
			expected: []string{"Supercalifragilisticexpialidocious"},
		},
		{
			name:     "empty string",
			input:    "",
			maxWidth: 80,
			expected: []string{""},
		},
		{
			name:     "multiple spaces between words",
			input:    "Multiple   spaces   between   words",
			maxWidth: 20,
			expected: []string{"Multiple spaces", "between words"},
		},
		{
			name:     "real world sentence wrap",
			input:    "Newly published APIs will use this authentication strategy unless overridden during publication.",
			maxWidth: 76,
			expected: []string{
				"Newly published APIs will use this authentication strategy unless overridden",
				"during publication.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WrapLongLine(tt.input, tt.maxWidth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWrapLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		expected []string
	}{
		{
			name:     "short line no wrap needed",
			input:    "Short line",
			maxWidth: 80,
			expected: []string{"Short line"},
		},
		{
			name:     "single sentence that fits",
			input:    "This is a single sentence that fits.",
			maxWidth: 80,
			expected: []string{"This is a single sentence that fits."},
		},
		{
			name:     "two short sentences that fit",
			input:    "First sentence. Second sentence.",
			maxWidth: 80,
			expected: []string{"First sentence. Second sentence."},
		},
		{
			name:     "two sentences that need splitting",
			input:    "First sentence. Second sentence.",
			maxWidth: 20,
			expected: []string{"First sentence.", "Second sentence."},
		},
		{
			name:     "sentence split across lines",
			input:    "This is a very long sentence that definitely needs to be wrapped across multiple lines.",
			maxWidth: 40,
			expected: []string{
				"This is a very long sentence that",
				"definitely needs to be wrapped across",
				"multiple lines.",
			},
		},
		{
			name:     "multiple sentences with wrapping",
			input:    "First short sentence. This is a much longer second sentence that will need wrapping.",
			maxWidth: 40,
			expected: []string{
				"First short sentence.",
				"This is a much longer second sentence",
				"that will need wrapping.",
			},
		},
		{
			name:     "real world portal description",
			input:    "The default authentication strategy for APIs published to the portal. Newly published APIs will use this authentication strategy unless overridden during publication. If set to `null`, API publications will not use an authentication strategy unless set during publication.",
			maxWidth: 76,
			expected: []string{
				"The default authentication strategy for APIs published to the portal.",
				"Newly published APIs will use this authentication strategy unless overridden",
				"during publication.",
				"If set to `null`, API publications will not use an authentication strategy",
				"unless set during publication.",
			},
		},
		{
			name:     "empty string",
			input:    "",
			maxWidth: 80,
			expected: []string{""},
		},
		{
			name:     "abbreviations with periods not split when fits",
			input:    "Use the e.g. example here. And another sentence.",
			maxWidth: 80,
			expected: []string{"Use the e.g. example here. And another sentence."},
		},
		{
			name:     "abbreviations with periods split when needed",
			input:    "Use the e.g. example here. And another sentence.",
			maxWidth: 30,
			expected: []string{"Use the e.g.", "example here.", "And another sentence."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WrapLine(tt.input, tt.maxWidth)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWrapLine_MaxWidthEnforced(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
	}{
		{
			name:     "long text",
			input:    "The default visibility of APIs in the portal. If set to `public`, newly published APIs are visible to unauthenticated developers. If set to `private`, newly published APIs are hidden from unauthenticated developers.",
			maxWidth: 76,
		},
		{
			name:     "very narrow width",
			input:    "This is a test sentence. Another one here.",
			maxWidth: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WrapLine(tt.input, tt.maxWidth)
			for _, line := range result {
				// Allow single words to exceed maxWidth
				if len(line) > tt.maxWidth {
					words := len(strings.Fields(line))
					assert.Equal(t, 1, words, "only single words should exceed maxWidth, got: %q", line)
				}
			}
		})
	}
}
