package isolated

import (
	"context"
	"testing"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
)

func TestExampleUDPRoute(t *testing.T) {
	udpRouteExampleManifests := samplesManifestPath("gateway-udproute.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyGatewayAPI).
		WithLabel(testlabels.Kind, testlabels.KindUDPRoute).
		WithSetup("create test namespace", featureSetup(withoutGateway())).
		Assess("deploying to cluster works and udp traffic is routed to the service",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				applyManifest(ctx, t, udpRouteExampleManifests)
				ctx = waitForGateway(ctx, t, k8stypes.NamespacedName{Namespace: GetNamespaceForT(ctx, t), Name: "example-udproute"})
				proxyUDPURL := GetUDPURLFromCtx(ctx)

				t.Logf("verifying that the UDPRoute becomes routable")
				assertEventuallyResponseUDP(t, proxyUDPURL, "udproute-example-manifest")

				return ctx
			}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
