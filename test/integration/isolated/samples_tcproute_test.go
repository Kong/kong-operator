package isolated

import (
	"context"
	"testing"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
)

func TestExampleTCPRoute(t *testing.T) {
	tcpRouteExampleManifests := samplesManifestPath("gateway-tcproute.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyGatewayAPI).
		WithLabel(testlabels.Kind, testlabels.KindTCPRoute).
		WithSetup("create test namespace", featureSetup(withoutGateway())).
		Assess("deploying to cluster works and tcp traffic is routed to the service",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				applyManifest(ctx, t, tcpRouteExampleManifests)
				ctx = waitForGateway(ctx, t, k8stypes.NamespacedName{Namespace: GetNamespaceForT(ctx, t), Name: "example-tcproute"})
				proxyTCPURL := GetTCPURLFromCtx(ctx)

				t.Logf("verifying that the TCPRoute becomes routable")
				assertEventuallyResponseTCP(t, proxyTCPURL, "tcproute-example-manifest")

				return ctx
			}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
