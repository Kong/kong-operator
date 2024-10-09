package ops_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/controller/konnect/ops"
)

func TestServerURL(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid URL",
			input:    "https://localhost:8000",
			expected: "https://localhost:8000",
		},
		{
			name:     "valid hostname",
			input:    "konghq.server.somewhere",
			expected: "https://konghq.server.somewhere",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ops.NewServerURL(tc.input)
			require.Equal(t, tc.expected, got.String())
		})
	}
}
