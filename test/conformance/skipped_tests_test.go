package conformance

import (
	"sigs.k8s.io/gateway-api/conformance/tests"

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
