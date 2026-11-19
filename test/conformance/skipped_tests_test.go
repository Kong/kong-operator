package conformance

import (
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/pkg/features"

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

// skippedTestsForStandardExpressionsRouter are ShortNames of tests skipped only
// for the standard gateway with the expressions router flavor.
var skippedTestsForStandardExpressionsRouter = []string{
	// Listener port isolation is implemented with `net.dst.port`, which Kong
	// takes from the Host header port when present (e.g. `Host: very.specific.com:1234`
	// on a listener at port 80), so such requests don't match their routes.
	// A dedicated listener-port predicate is needed in Kong:
	// https://konghq.aha.io/ideas/ideas/GTWY-I-1612
	// and for more context https://konghq.atlassian.net/browse/FTI-8001.
	tests.HTTPRouteHostnameIntersection.ShortName,
}

var skippedTestsForHybrid = []string{

	// Extended profile.
	tests.HTTPRouteQueryParamMatching.ShortName,
}

// skippedTestsForConfig returns the list of skipped tests for the given router
// flavor, gateway type and conformance shard.
func skippedTestsForConfig(
	routerFlavor consts.RouterFlavor,
	gwType gatewayType,
	supportedFeatures []features.FeatureName,
	shard test.ConformanceShard,
) []string {
	skipped := append([]string{}, skippedTestsShared...)
	if gwType == standardGateway {
		skipped = append(skipped, skippedTestsForStandard...)
		if routerFlavor == consts.RouterFlavorTraditionalCompatible {
			skipped = append(skipped, skippedTestsForStandardTraditionalCompatibleRouter...)
		}
	}

	if gwType == standardGateway && routerFlavor == consts.RouterFlavorExpressions {
		skipped = append(skipped, skippedTestsForStandardExpressionsRouter...)
	}

	if gwType == hybridGateway {
		skipped = append(skipped, skippedTestsForHybrid...)
	}

	// Allow excluding extra (e.g. flaky or undesired) tests via the
	// KONG_TEST_CONFORMANCE_SKIP_TESTS environment variable so a local run can
	// drop the gotest -run filter and still avoid known-bad tests.
	skipped = append(skipped, test.ConformanceSkipTests()...)

	skipped = append(skipped, skippedByShard(shard, supportedFeatures, skipped)...)

	return skipped
}

// skippedByShard returns the ShortNames of the conformance tests that don't
// belong to shard.
//
// Tests are sorted by name and distributed round-robin so that every shard
// gets a similar mix of tests. Tests that are already skipped or that aren't
// supported by the given gateway/router configuration are filtered out first,
// so the shards are balanced by the tests that actually run.
func skippedByShard(
	shard test.ConformanceShard,
	supportedFeatures []features.FeatureName,
	alreadySkipped []string,
) []string {
	if !shard.Enabled() {
		return nil
	}

	skipped := sets.New(alreadySkipped...)
	supported := sets.New(supportedFeatures...)
	runnable := make([]string, 0, len(tests.ConformanceTests))
	for _, conformanceTest := range tests.ConformanceTests {
		if skipped.Has(conformanceTest.ShortName) || !supported.HasAll(conformanceTest.Features...) {
			continue
		}
		runnable = append(runnable, conformanceTest.ShortName)
	}
	slices.Sort(runnable)

	notInShard := make([]string, 0, len(runnable))
	for i, name := range runnable {
		if i%shard.Total != shard.Index-1 {
			notInShard = append(notInShard, name)
		}
	}
	return notInShard
}
