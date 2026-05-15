package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

var skippedTestsShared = []string{
	// NOTE:
	// Issue tracking all gRPC related conformance tests:
	// https://github.com/Kong/kong-operator/issues/2345
	// Tests GRPCRouteHeaderMatching, GRPCExactMethodMatching, GRPCRouteWeight are skipped
	// because to proxy different gRPC services and route requests based on Header or Method,
	// it is necessary to create separate catch-all routes for them.
	// However, Kong does not define priority behavior in this situation unless priorities are manually added.
	tests.GRPCRouteHeaderMatching.ShortName,
	tests.GRPCExactMethodMatching.ShortName,
	tests.GRPCRouteWeight.ShortName,
	// When processing this scenario, the Kong's router requires `priority` to be specified for routes.
	// We cannot provide that for routes that are part of the conformance suite.
	tests.GRPCRouteListenerHostnameMatching.ShortName,

	// TLSRoute tests that cannot pass yet.
	tests.TLSRouteListenerMixedTerminationNotSupported.ShortName,
}

var skippedTestsForExpressionsRouter = []string{}

var skippedTestsForTraditionalCompatibleRouter = []string{
	// HTTPRoute
	tests.HTTPRouteHeaderMatching.ShortName,
}

var skippedTestsForHybrid = []string{

	// Core profile.
	tests.HTTPRouteHTTPSListener.ShortName,
	tests.HTTPRouteInvalidNonExistentBackendRef.ShortName,
	tests.HTTPRouteListenerHostnameMatching.ShortName,
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteMethodMatching.ShortName,
	tests.HTTPRouteMatchingAcrossRoutes.ShortName,
	tests.HTTPRoutePathMatchOrder.ShortName,
	tests.HTTPRouteQueryParamMatching.ShortName,
	tests.GatewayModifyListeners.ShortName,
	tests.GatewayObservedGenerationBump.ShortName,
	tests.GatewaySecretReferenceGrantAllInNamespace.ShortName,
	tests.GatewaySecretReferenceGrantSpecific.ShortName,
	tests.GatewayWithAttachedRoutes.ShortName,

	// Extended profile.
	tests.HTTPRouteRewriteHost.ShortName,
	tests.HTTPRouteRewritePath.ShortName,

	// This test is skipped because it proven to be flaky.
	// TODO: https://github.com/Kong/kong-operator/issues/2793
	tests.HTTPRouteReferenceGrant.ShortName,
}

// skippedTestsForConfig returns the list of skipped tests for the given router flavor and gateway type.
func skippedTestsForConfig(routerFlavor consts.RouterFlavor, gwType gatewayType) []string {
	skipped := append([]string{}, skippedTestsShared...)

	switch routerFlavor {
	case consts.RouterFlavorTraditionalCompatible:
		skipped = append(skipped, skippedTestsForTraditionalCompatibleRouter...)
		if gwType == standardGateway {
			skipped = append(skipped, tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName)
		}
	case consts.RouterFlavorExpressions:
		skipped = append(skipped, skippedTestsForExpressionsRouter...)
	}

	if gwType == hybridGateway {
		skipped = append(skipped, skippedTestsForHybrid...)
	}

	return skipped
}
