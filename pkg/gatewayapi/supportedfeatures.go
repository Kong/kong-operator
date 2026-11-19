package gatewayapi

import (
	"errors"
	"slices"

	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	traditionalCompatibleRouterSupportedFeatures = slices.Clone(commonSupportedFeatures)

	expressionsRouterSupportedFeatures = append(slices.Clone(commonSupportedFeatures),
		// HTTPRoute extended.
		features.SupportHTTPRouteMethodMatching,
		features.SupportHTTPRouteQueryParamMatching,
		// Routes attached to specific listener ports are restricted by a `net.dst.port`
		// predicate, which only expression routes support. It is therefore reported for
		// the expressions router flavor only: with the traditional and
		// traditional_compatible flavors a route would match on every listener port.
		// Kong's traditional routers have no way to express such a predicate, so this
		// can't be supported for them and that won't change (Gateway won't support it).
		//
		// Known limitation: Kong takes `net.dst.port` from the Host header port when the
		// header carries one, otherwise from the listener port. A request such as
		// `Host: example.com:1234` sent to a listener on port 80 therefore doesn't match
		// the route. This is why the HTTPRouteHostnameIntersection conformance test is
		// skipped for the expressions router.
		//
		// The feature is still advertised deliberately. The limitation only affects requests
		// whose Host header carries a port other than the listener's.  A predicate that is
		// always the listener port is needed in Kong to lift it:
		// https://konghq.aha.io/ideas/ideas/GTWY-I-1612
		// and for more context https://konghq.atlassian.net/browse/FTI-8001.
		features.SupportHTTPRouteParentRefPort,
	)
)

var commonSupportedFeatures = []features.FeatureName{
	// Core features.
	features.SupportGateway,
	features.SupportHTTPRoute,
	features.SupportTLSRoute,
	features.SupportTCPRoute,
	features.SupportGRPCRoute,
	features.SupportUDPRoute,
	features.SupportReferenceGrant,

	// Gateway extended.
	features.SupportGatewayAddressEmpty,
	features.SupportGatewayPort8080,
	features.SupportGatewayInfrastructure,

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

// GetSupportedFeatures returns the supported features for the router type and gateway mode.
func GetSupportedFeatures(routerType consts.RouterFlavor, isHybrid bool) ([]features.FeatureName, error) {
	// Return a clone so callers cannot mutate the package-level slices
	// (e.g. via slices.Sort), which are shared across all callers and would
	// otherwise cause data races.
	switch routerType {
	case consts.RouterFlavorTraditionalCompatible:
		supportedFeatures := slices.Clone(traditionalCompatibleRouterSupportedFeatures)
		if isHybrid {
			supportedFeatures = append(supportedFeatures, features.SupportHTTPRouteMethodMatching)
		}
		return supportedFeatures, nil
	case consts.RouterFlavorExpressions:
		return slices.Clone(expressionsRouterSupportedFeatures), nil
	default:
		return nil, errors.New("unsupported router type")
	}
}
