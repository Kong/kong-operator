package server_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

func TestServer(t *testing.T) {
	testCases := []struct {
		name                  string
		input                 string
		expectedURL           string
		expectedRegion        server.Region
		expectedErrorContains string
	}{
		{
			name:           "valid URL",
			input:          "https://us.konghq.com:8000",
			expectedURL:    "https://us.konghq.com:8000",
			expectedRegion: server.Region("us"),
		},
		{
			name:           "valid hostname",
			input:          "us.konghq.com",
			expectedURL:    "https://us.konghq.com",
			expectedRegion: server.Region("us"),
		},
		{
			name:           "valid hostname with yet-unknown region",
			input:          "pl.konghq.com",
			expectedURL:    "https://pl.konghq.com",
			expectedRegion: server.Region("pl"),
		},
		{
			name:           "valid hostname with global region",
			input:          "global.konghq.com",
			expectedURL:    "https://global.konghq.com",
			expectedRegion: server.RegionGlobal,
		},
		{
			name:                  "invalid URL",
			input:                 "not-a-valid-url:\\us.konghq.com",
			expectedErrorContains: "failed to parse region from hostname",
		},
		{
			name:                  "region not satisfying regex",
			input:                 "not-two-lowercase-letters.konghq.com",
			expectedErrorContains: `failed to parse region from hostname: invalid region "not-two-lowercase-letters"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := server.NewServer[konnectv1alpha2.KonnectGatewayControlPlane](tc.input)
			if tc.expectedErrorContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectedErrorContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedURL, got.URL())
			assert.Equal(t, tc.expectedRegion, got.Region())
		})
	}

	t.Run("KonnectCloudGatewayNetwork", func(t *testing.T) {
		konnectTestCases := []struct {
			name           string
			input          string
			expectedURL    string
			expectedRegion server.Region
		}{
			{
				name:           "us",
				input:          "us.api.konghq.com",
				expectedURL:    "https://global.api.konghq.com",
				expectedRegion: server.RegionGlobal,
			},
			{
				name:           "eu",
				input:          "eu.api.konghq.com",
				expectedURL:    "https://global.api.konghq.com",
				expectedRegion: server.RegionGlobal,
			},
		}
		for _, tc := range konnectTestCases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := server.NewServer[konnectv1alpha1.KonnectCloudGatewayNetwork](tc.input)
				require.NoError(t, err)
				assert.Equal(t, tc.expectedURL, got.URL())
				assert.Equal(t, tc.expectedRegion, got.Region())
			})
		}
	})
}

func TestServer_Domain(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDomain string
	}{
		{
			name:       "standard Konnect URL",
			input:      "https://us.api.konghq.com",
			wantDomain: "konghq.com",
		},
		{
			name:       "eu region",
			input:      "https://eu.api.konghq.com",
			wantDomain: "konghq.com",
		},
		{
			name:       "bare hostname with region only",
			input:      "us.konghq.com",
			wantDomain: "konghq.com",
		},
		{
			name:       "custom domain with many labels",
			input:      "us.gateway.internal.example.com",
			wantDomain: "example.com",
		},
		{
			name:       "hostname with single label after region",
			input:      "us.konghq",
			wantDomain: "konghq",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := server.NewServer[konnectv1alpha2.KonnectGatewayControlPlane](tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.wantDomain, s.Domain())
		})
	}
}
