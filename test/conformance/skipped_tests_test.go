package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test"
)

var skippedTestsShared = []string{
	// newly added in gateway api v1.6.0-rc.1, https://github.com/Kong/kong-operator/issues/4662
	tests.GatewayListenerUnsupportedProtocol.ShortName,
	tests.GatewayInvalidParametersRef.ShortName,
	tests.HTTPRouteNoBackendRefs.ShortName,

	// failed after bumping gateway api to v1.6.0-rc.1, https://github.com/Kong/kong-operator/issues/4661
	tests.HTTPRouteWeight.ShortName,
}

var skippedTestsForExpressionsRouter = []string{}

var skippedTestsForTraditionalCompatibleRouter = []string{
	// HTTPRoute
	tests.HTTPRouteHeaderMatching.ShortName,
}

var skippedTestsForHybrid = []string{

	// Core profile.
	tests.HTTPRouteMethodMatching.ShortName,
	tests.HTTPRouteQueryParamMatching.ShortName,

	// Extended profile.
	tests.HTTPRouteRewriteHost.ShortName,
	tests.HTTPRouteRewritePath.ShortName,

	tests.GRPCExactMethodMatching.ShortName,
	tests.GRPCRouteHeaderMatching.ShortName,
	tests.GRPCRouteListenerHostnameMatching.ShortName,
	tests.GRPCRouteWeight.ShortName,
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

	// Allow excluding extra (e.g. flaky or undesired) tests via the
	// KONG_TEST_CONFORMANCE_SKIP_TESTS environment variable so a local run can
	// drop the gotest -run filter and still avoid known-bad tests.
	skipped = append(skipped, test.ConformanceSkipTests()...)

	return skipped
}
