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
			supportedFeatures, err := GetSupportedFeatures(routerFlavor)
			require.NoError(t, err)
			require.Contains(t, supportedFeatures, features.SupportTCPRoute)
		})
	}
}

func TestGetSupportedFeaturesHTTPRouteParentRefPort(t *testing.T) {
	expressions, err := GetSupportedFeatures(consts.RouterFlavorExpressions)
	require.NoError(t, err)
	require.Contains(t, expressions, features.SupportHTTPRouteParentRefPort)

	traditionalCompatible, err := GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	require.NoError(t, err)
	require.NotContains(t, traditionalCompatible, features.SupportHTTPRouteParentRefPort)
}
