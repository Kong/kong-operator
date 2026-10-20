package isolated

import (
	"context"
	"net/http"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

func TestIngressHTTPSExample(t *testing.T) {
	ingressExampleManifests := samplesManifestPath("ingress-https.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyIngress).
		WithLabel(testlabels.Kind, testlabels.KindIngress).
		WithSetup("deploy Gateway managed by Kong Operator", featureSetup()).
		Assess("deploying to cluster works and HTTPS traffic is routed to the service",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				var (
					proxyURLHTTP  = GetHTTPURLFromCtx(ctx)
					proxyURLHTTPS = GetHTTPSURLFromCtx(ctx)
				)

				applyManifest(ctx, t, ingressExampleManifests, "ingressClassName: kong", "ingressClassName: "+GetIngressClassFromCtx(ctx))

				t.Logf("verifying that the Ingress routes traffic properly")
				helpers.EventuallyGETPath(
					t,
					proxyURLHTTPS,
					proxyURLHTTPS.String(),
					"/",
					&helpers.HTTPSOptions{
						InsecureSkipVerify: true,
					},
					http.StatusOK,
					"<title>httpbin.org</title>",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				t.Logf("verifying that traffic is not served over HTTP")
				helpers.EventuallyGETPath(
					t,
					proxyURLHTTP,
					proxyURLHTTP.String(),
					"/",
					nil,
					// Status code returned by Kong when HTTPS is required but request is made over HTTP.
					http.StatusUpgradeRequired,
					"",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				return ctx
			}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}

func TestIngressHTTPExample(t *testing.T) {
	ingressExampleManifests := samplesManifestPath("ingress-http.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyIngress).
		WithLabel(testlabels.Kind, testlabels.KindIngress).
		WithSetup("deploy Gateway managed by Kong Operator", featureSetup()).
		Assess("deploying to cluster works and HTTP traffic is routed to the service",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				proxyURLHTTP := GetHTTPURLFromCtx(ctx)

				applyManifest(ctx, t, ingressExampleManifests, "ingressClassName: kong", "ingressClassName: "+GetIngressClassFromCtx(ctx))

				t.Logf("verifying that the Ingress routes traffic properly")
				helpers.EventuallyGETPath(
					t,
					proxyURLHTTP,
					proxyURLHTTP.String(),
					"/",
					nil,
					http.StatusOK,
					"<title>httpbin.org</title>",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				return ctx
			}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
