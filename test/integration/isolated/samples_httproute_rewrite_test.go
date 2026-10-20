package isolated

import (
	"context"
	"net/http"
	"testing"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

func TestHTTPRouteRewriteExample(t *testing.T) {
	httprouteURLRewriteExampleManifests := samplesManifestPath("gateway-httproute-rewrite-path.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyGatewayAPI).
		WithLabel(testlabels.Kind, testlabels.KindHTTPRoute).
		WithSetup("create test namespace", featureSetup(withoutGateway())).
		Assess("deploying to cluster works and HTTP requests get properly rewritten URI",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				applyManifest(ctx, t, httprouteURLRewriteExampleManifests)
				ctx = waitForGateway(ctx, t, k8stypes.NamespacedName{Namespace: GetNamespaceForT(ctx, t), Name: "kong"})
				proxyURL := GetHTTPURLFromCtx(ctx)

				t.Logf("verifying that the HTTPRoute routes traffic properly")

				t.Logf("asserting /full-path-prefix path is redirected to /echo?msg=hello from the manifest")
				helpers.EventuallyGETPath(
					t,
					proxyURL,
					proxyURL.String(),
					"/full-path-prefix",
					nil,
					http.StatusOK,
					"hello",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				t.Logf("asserting /old-prefix?msg=hello path is redirected to /echo?msg=hello replacing the prefix")
				helpers.EventuallyGETPath(
					t,
					proxyURL,
					proxyURL.String(),
					"/old-prefix?msg=hello",
					nil,
					http.StatusOK,
					"hello",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				return ctx
			}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
