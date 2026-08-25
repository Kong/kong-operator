package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

	"github.com/kong/kong-operator/v2/test"
)

var skippedTestsShared = []string{}

var skippedTestsForStandard = []string{
	// TODO: https://github.com/kubernetes-sigs/gateway-api/issues/5121
	// The readiness probe can establish a TCP connection before the TCPRoute
	// configuration reaches the data plane, causing the test to fail with EOF.
	tests.TCPRouteWeightedRouting.ShortName,
}

var skippedTestsForHybrid = []string{

	// Core profile.
	tests.HTTPRouteMethodMatching.ShortName,
	tests.HTTPRouteQueryParamMatching.ShortName,
}

// skippedTestsForConfig returns the list of skipped tests for the given gateway type.
func skippedTestsForConfig(gwType gatewayType) []string {
	skipped := append([]string{}, skippedTestsShared...)
	if gwType == standardGateway {
		skipped = append(skipped, skippedTestsForStandard...)
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
