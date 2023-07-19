//go:build conformance_tests

package conformance

import (
	"flag"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
)

const (
	showDebug                  = true
	enableAllSupportedFeatures = true
)

var shouldCleanup = flag.Bool("cleanup", true, "indicates whether or not the base test resources such as Gateways should be cleaned up after the run.")

func TestGatewayConformance(t *testing.T) {
	t.Skip() // TODO: https://github.com/Kong/gateway-operator/issues/11

	t.Parallel()

	t.Log("creating GatewayClass for gateway conformance tests")
	gwc := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, clients.MgrClient.Create(ctx, gwc))
	defer func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, gwc))
	}()

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Log("starting the gateway conformance test suite")
	conformanceTestsBaseManifests := fmt.Sprintf("%s/conformance/base/manifests.yaml", testutils.GatewayRawRepoURL)

	cSuite := suite.New(suite.Options{
		Client:                     clients.MgrClient,
		GatewayClassName:           gwc.Name,
		Debug:                      showDebug,
		CleanupBaseResources:       *shouldCleanup,
		BaseManifests:              conformanceTestsBaseManifests,
		EnableAllSupportedFeatures: enableAllSupportedFeatures,
		SkipTests: []string{
			// core conformance
			// Gateway
			tests.GatewayInvalidRouteKind.ShortName,
			tests.GatewayInvalidTLSConfiguration.ShortName,
			tests.GatewayObservedGenerationBump.ShortName,
			tests.GatewayWithAttachedRoutes.ShortName,
			tests.GatewaySecretInvalidReferenceGrant.ShortName,
			tests.GatewaySecretMissingReferenceGrant.ShortName,
			tests.GatewaySecretReferenceGrantAllInNamespace.ShortName,
			// HTTPRoute
			tests.HTTPRouteCrossNamespace.ShortName,
			tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,
			tests.HTTPRouteInvalidCrossNamespaceParentRef.ShortName,
			tests.HTTPRouteInvalidParentRefNotMatchingListenerPort.ShortName,
			tests.HTTPRoutePartiallyInvalidViaInvalidReferenceGrant.ShortName,
			tests.HTTPRouteReferenceGrant.ShortName,

			// this test is currently fixed but cannot be re-enabled yet due to an upstream issue
			// https://github.com/kubernetes-sigs/gateway-api/pull/1745
			tests.GatewaySecretReferenceGrantSpecific.ShortName,

			// standard conformance
			tests.HTTPRouteHeaderMatching.ShortName,

			// extended conformance
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3680
			tests.GatewayClassObservedGenerationBump.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3678
			tests.TLSRouteSimpleSameNamespace.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3679
			tests.HTTPRouteQueryParamMatching.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3681
			tests.HTTPRouteRedirectPort.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3682
			tests.HTTPRouteRedirectScheme.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3683
			tests.HTTPRouteResponseHeaderModifier.ShortName,

			// experimental conformance
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3684
			tests.HTTPRouteRedirectPath.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3685
			tests.HTTPRouteRewriteHost.ShortName,
			// https://github.com/Kong/kubernetes-ingress-controller/issues/3686
			tests.HTTPRouteRewritePath.ShortName,
		},
	})
	cSuite.Setup(t)
	cSuite.Run(t, tests.ConformanceTests)
}
