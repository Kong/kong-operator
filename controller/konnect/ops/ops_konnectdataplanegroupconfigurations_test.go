package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/controller/konnect/server"
)

func TestServerRegionToSDKControlPlaneGeoForCloudGateway(t *testing.T) {
	testCases := []struct {
		region                server.Region
		expected              sdkkonnectcomp.ControlPlaneGeo
		expectedErrorContains string
	}{
		{
			region:                server.RegionGlobal,
			expectedErrorContains: "global region is not supported for Cloud Gateway operations",
		},
		{
			region:   server.RegionUS,
			expected: sdkkonnectcomp.ControlPlaneGeoUs,
		},
		{
			region:   server.RegionEU,
			expected: sdkkonnectcomp.ControlPlaneGeoEu,
		},
		{
			region:   server.RegionME,
			expected: sdkkonnectcomp.ControlPlaneGeoMe,
		},
		{
			region:   server.RegionAU,
			expected: sdkkonnectcomp.ControlPlaneGeoAu,
		},
		{
			region:   server.RegionIN,
			expected: sdkkonnectcomp.ControlPlaneGeoIn,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.region.String(), func(t *testing.T) {
			got, err := serverRegionToSDKControlPlaneGeoForCloudGateway(tc.region)
			if tc.expectedErrorContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectedErrorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, got)
		})
	}
}
