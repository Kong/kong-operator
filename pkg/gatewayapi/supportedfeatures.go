package gatewayapi

import (
	"errors"
	"slices"

	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	traditionalCompatibleRouterSupportedFeatures = slices.Clone(commonSupportedFeatures) // Append here the traditional compatible router specific features.

	expressionsRouterSupportedFeatures = append(slices.Clone(commonSupportedFeatures),
		// HTTPRoute extended.
		features.SupportHTTPRouteMethodMatching,
		features.SupportHTTPRouteQueryParamMatching,
	)
)

var commonSupportedFeatures = []features.FeatureName{
	// Core features.
	features.SupportGateway,
	features.SupportHTTPRoute,
	features.SupportTLSRoute,
	features.SupportGRPCRoute,
	features.SupportReferenceGrant,

	// Gateway extended.
	features.SupportGatewayAddressEmpty,
	features.SupportGatewayPort8080,
	features.SupportGatewayInfrastructurePropagation,

	// HTTPRoute extended.
	features.SupportHTTPRouteResponseHeaderModification,
	features.SupportHTTPRoutePathRewrite,
	features.SupportHTTPRouteHostRewrite,
	features.SupportHTTPRouteBackendTimeout,

	// TLSRoute extended.
	features.SupportTLSRouteModeTerminate,
	// TODO: support multiple TLSRoute modes on the same port:
	// https://github.com/Kong/kong-operator/issues/3511
	// features.SupportTLSRouteModeMixed,
}

// GetSupportedFeatures returns the supported features for the given router type.
// The returned slice is safe for callers to mutate.
func GetSupportedFeatures(routerType consts.RouterFlavor) ([]features.FeatureName, error) {
	// Return a clone so callers cannot mutate the package-level slices
	// (e.g. via slices.Sort), which are shared across all callers and would
	// otherwise cause data races.
	switch routerType {
	case consts.RouterFlavorTraditionalCompatible:
		return slices.Clone(traditionalCompatibleRouterSupportedFeatures), nil
	case consts.RouterFlavorExpressions:
		return slices.Clone(expressionsRouterSupportedFeatures), nil
	default:
		return nil, errors.New("unsupported router type")
	}
}
