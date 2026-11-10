package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test"
)

// skippedTestsShared are ShortNames of tests need to be skipped for both Standard and Hybrid.
var skippedTestsShared = []string{}

var skippedTestsForStandard = []string{}

// skippedTestsForStandardTraditionalCompatibleRouter are ShortNames of tests
// skipped only for the standard gateway with the traditional_compatible router
// flavor.
var skippedTestsForStandardTraditionalCompatibleRouter = []string{
	// The standard gateway does not preserve Gateway API method-over-header
	// match precedence with the traditional_compatible router flavor, so
	// HTTPRouteMethodMatching is enabled for the hybrid gateway only.
	tests.HTTPRouteMethodMatching.ShortName,
}

var skippedTestsForHybrid = []string{

	// Extended profile.
	tests.HTTPRouteQueryParamMatching.ShortName,
}

// skippedTestsForConfig returns the list of skipped tests for the given router flavor and gateway type.
func skippedTestsForConfig(routerFlavor consts.RouterFlavor, gwType gatewayType) []string {
	skipped := append([]string{}, skippedTestsShared...)
	if gwType == standardGateway {
		skipped = append(skipped, skippedTestsForStandard...)
		if routerFlavor == consts.RouterFlavorTraditionalCompatible {
			skipped = append(skipped, skippedTestsForStandardTraditionalCompatibleRouter...)
		}
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
