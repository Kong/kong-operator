package gatewayapi

import (
	"errors"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	traditionalCompatibleRouterSupportedFeatures = commonSupportedFeatures.Clone().Insert(
	// Add here the traditional compatible router specific features.
	)

	expressionsRouterSupportedFeatures = commonSupportedFeatures.Clone().Insert(
		// HTTPRoute extended.
		features.SupportHTTPRouteMethodMatching,
		features.SupportHTTPRouteQueryParamMatching,
	)
)

var commonSupportedFeatures = sets.New(
	// Core features.
	features.SupportGateway,
	features.SupportHTTPRoute,
	features.SupportGRPCRoute,
	features.SupportReferenceGrant,

	// Gateway extended.
	features.SupportGatewayPort8080,

	// HTTPRoute extended.
	features.SupportHTTPRouteResponseHeaderModification,
	features.SupportHTTPRoutePathRewrite,
	features.SupportHTTPRouteHostRewrite,
	// TODO: https://github.com/Kong/kubernetes-ingress-controller/issues/5868
	// Temporarily disabled and tracking through the following issue.
	// features.SupportHTTPRouteBackendTimeout,
)

// GetSupportedFeatures returns the supported features for the given router type.
func GetSupportedFeatures(routerType consts.RouterFlavor) (sets.Set[features.FeatureName], error) {
	switch routerType {
	case consts.RouterFlavorTraditionalCompatible:
		return traditionalCompatibleRouterSupportedFeatures, nil
	case consts.RouterFlavorExpressions:
		return expressionsRouterSupportedFeatures, nil
	default:
		return nil, errors.New("unsupported router type")
	}
}
