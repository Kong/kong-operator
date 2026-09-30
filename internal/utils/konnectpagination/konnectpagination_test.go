package konnectpagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageAfterCursorFromNextPageURL(t *testing.T) {
	tests := []struct {
		name      string
		next      string
		expected  string
		expectErr bool
	}{
		{
			name:     "next URI carrying a page[after] cursor",
			next:     "https://us.api.konghq.com/v1/mcp-cp/cp-id/mcp-servers?page%5Bafter%5D=cursor-1&page%5Bsize%5D=10",
			expected: "cursor-1",
		},
		{
			name:     "relative next URI carrying a page[after] cursor",
			next:     "/v1/mcp-cp/cp-id/mcp-servers?page%5Bafter%5D=cursor-1",
			expected: "cursor-1",
		},
		{
			name:      "next URI without a page[after] parameter",
			next:      "https://us.api.konghq.com/v1/mcp-cp/cp-id/mcp-servers",
			expectErr: true,
		},
		{
			name:      "next URI with an empty page[after] parameter",
			next:      "https://us.api.konghq.com/v1/mcp-cp/cp-id/mcp-servers?page%5Bafter%5D=",
			expectErr: true,
		},
		{
			name:      "unparseable next URI",
			next:      "https://example.com/%zz",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PageAfterCursorFromNextPageURL(tt.next)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
