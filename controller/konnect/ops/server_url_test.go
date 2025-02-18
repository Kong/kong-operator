package ops_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/controller/konnect/ops"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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
			got := ops.NewServerURL[konnectv1alpha1.KonnectGatewayControlPlane](tc.input)
			require.Equal(t, tc.expected, got.String())
		})
	}

	t.Run("KonnectCloudGatewayNetwork", func(t *testing.T) {
		konnectTestCases := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "us",
				input:    "us.api.konghq.com",
				expected: "https://global.api.konghq.com",
			},
			{
				name:     "eu",
				input:    "eu.api.konghq.com",
				expected: "https://global.api.konghq.com",
			},
		}
		for _, tc := range konnectTestCases {
			t.Run(tc.name, func(t *testing.T) {
				got := ops.NewServerURL[konnectv1alpha1.KonnectCloudGatewayNetwork](tc.input)
				require.Equal(t, tc.expected, got.String())
			})
		}
	})
}
