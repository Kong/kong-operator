package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test"
)

// skippedTestsShared are ShortNames of tests need to be skipped for both Standard and Hybrid.
var skippedTestsShared = []string{}

var skippedTestsForStandard = []string{}

var skippedTestsForHybrid = []string{

	// Extended profile.
	tests.HTTPRouteMethodMatching.ShortName,
	tests.HTTPRouteQueryParamMatching.ShortName,
}

var skippedTestsForExpressionsRouter = []string{
	// Expression routes match the listener port with `net.dst.port`, which Kong
	// takes from the port in the Host header when present. A request with
	// `Host: very.specific.com:1234` doesn't match a route for a listener on
	// port 80, while the test requires the Host header port to be ignored.
	tests.HTTPRouteHostnameIntersection.ShortName,
}

// skippedTestsForConfig returns the list of skipped tests for the given gateway type and router flavor.
func skippedTestsForConfig(gwType gatewayType, routerFlavor consts.RouterFlavor) []string {
	skipped := append([]string{}, skippedTestsShared...)
	if gwType == standardGateway {
		skipped = append(skipped, skippedTestsForStandard...)
	}

	if gwType == hybridGateway {
		skipped = append(skipped, skippedTestsForHybrid...)
	}

	if routerFlavor == consts.RouterFlavorExpressions {
		skipped = append(skipped, skippedTestsForExpressionsRouter...)
	}

	// Allow excluding extra (e.g. flaky or undesired) tests via the
	// KONG_TEST_CONFORMANCE_SKIP_TESTS environment variable so a local run can
	// drop the gotest -run filter and still avoid known-bad tests.
	skipped = append(skipped, test.ConformanceSkipTests()...)

	return skipped
}
