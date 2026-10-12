package gatewayapi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGetSupportedFeaturesIncludesTCPRoute(t *testing.T) {
	for _, routerFlavor := range []consts.RouterFlavor{
		consts.RouterFlavorTraditionalCompatible,
		consts.RouterFlavorExpressions,
	} {
		t.Run(string(routerFlavor), func(t *testing.T) {
			supportedFeatures, err := GetSupportedFeatures(routerFlavor, false)
			require.NoError(t, err)
			require.Contains(t, supportedFeatures, features.SupportTCPRoute)
		})
	}
}

func TestGetSupportedFeaturesMethodMatching(t *testing.T) {
	for _, tc := range []struct {
		name          string
		routerFlavor  consts.RouterFlavor
		isHybrid      bool
		methodSupport bool
	}{
		{"standard traditional-compatible", consts.RouterFlavorTraditionalCompatible, false, false},
		{"hybrid traditional-compatible", consts.RouterFlavorTraditionalCompatible, true, true},
		{"standard expressions", consts.RouterFlavorExpressions, false, true},
		{"hybrid expressions", consts.RouterFlavorExpressions, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			supportedFeatures, err := GetSupportedFeatures(tc.routerFlavor, tc.isHybrid)
			require.NoError(t, err)
			if tc.methodSupport {
				require.Contains(t, supportedFeatures, features.SupportHTTPRouteMethodMatching)
			} else {
				require.NotContains(t, supportedFeatures, features.SupportHTTPRouteMethodMatching)
			}

			originalFeature := supportedFeatures[0]
			supportedFeatures[0] = features.FeatureName("changed")
			freshFeatures, err := GetSupportedFeatures(tc.routerFlavor, tc.isHybrid)
			require.NoError(t, err)
			require.Equal(t, originalFeature, freshFeatures[0])
		})
	}
}

func TestGetSupportedFeaturesUnsupportedRouter(t *testing.T) {
	supportedFeatures, err := GetSupportedFeatures(consts.RouterFlavor("unsupported"), false)
	require.ErrorContains(t, err, "unsupported router type")
	require.Nil(t, supportedFeatures)
}
