package isolated

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util/builder"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

func TestHTTPRouteFilterRequestRedirect(t *testing.T) {
	f := features.
		New("essentials").
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyGatewayAPI).
		WithLabel(testlabels.Kind, testlabels.KindHTTPRoute).
		WithSetup("deploy Gateway managed by Kong Operator", featureSetup(
			withGatewayListeners(builder.NewListener("http").HTTP().WithPort(80).Build()),
		)).
		WithSetup("prepare gateway API client", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			t.Log("getting a gateway client")
			gatewayClient, err := gatewayclient.NewForConfig(GetClusterFromCtx(ctx).Config())
			assert.NoError(t, err)
			return SetInCtxForT(ctx, t, gatewayClient)
		}).
		Assess("Create an HTTPRoute redirecting to the httpbin service and verify if the request is redirected", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			gateway := GetFromCtxForT[*gatewayapi.Gateway](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			gatewayClient := GetFromCtxForT[*gatewayclient.Clientset](ctx, t)
			proxyHTTPURL := GetHTTPURLFromCtx(ctx)

			httpRoute := &gatewayapi.HTTPRoute{
				Name: "httproute-request-redirect-test",
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{{
							Name: gatewayapi.ObjectName(gateway.Name),
						}},
					},
					Rules: []gatewayapi.HTTPRouteRule{
						{
							Matches: []gatewayapi.HTTPRouteMatch{
								builder.NewHTTPRouteMatch().WithPathPrefix("/").Build(),
							},
							Filters: []gatewayapi.HTTPRouteFilter{
								builder.NewHTTPRouteRequestRedirectFilter().
									WithRequestRedirectStatusCode(http.StatusMovedPermanently).
									WithRequestRedirectHost("example.com").WithRequestRedirectPathModifier(
									gatewayapi.FullPathHTTPPathModifier,
									"/redirect-target",
								).Build(),
							},
						},
					},
				},
			}

			httpRoute, err := gatewayClient.GatewayV1().HTTPRoutes(namespace).Create(ctx, httpRoute, metav1.CreateOptions{})
			assert.NoError(t, err)

			t.Logf("Verify that replaceFullPath works")
			httpClientIgnoreRedirect := &http.Client{
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			assert.Eventually(
				t, func() bool {
					resp, err := httpClientIgnoreRedirect.Get(proxyHTTPURL.String())
					if err != nil {
						return false
					}
					defer resp.Body.Close()
					return resp.StatusCode == http.StatusMovedPermanently &&
						resp.Header.Get("Location") == "http://example.com/redirect-target"
				},
				consts.IngressWait, consts.WaitTick,
			)

			redirectPluginVersionRange, err := kong.NewRange(">=3.9.0")
			assert.NoError(t, err)
			kongVersion := kongVersionFromProxy(ctx, t, proxyHTTPURL)
			supportRedirectPlugin := redirectPluginVersionRange(kongVersion)
			if !supportRedirectPlugin {
				t.Logf("Skipping case for preserving path in request redirect because Kong version is %s which does not support redirect plugin", kongVersion)
				return ctx
			}

			t.Log("Update RequestRedirect filter of HTTPRoute to test preserving path in request redirect")
			httpRoute, err = gatewayClient.GatewayV1().HTTPRoutes(namespace).Get(ctx, httpRoute.Name, metav1.GetOptions{})
			assert.NoError(t, err)
			httpRoute.Spec.Rules = []gatewayapi.HTTPRouteRule{
				{
					Matches: []gatewayapi.HTTPRouteMatch{
						builder.NewHTTPRouteMatch().WithPathPrefix("/").Build(),
					},
					Filters: []gatewayapi.HTTPRouteFilter{
						builder.NewHTTPRouteRequestRedirectFilter().
							WithRequestRedirectStatusCode(http.StatusMovedPermanently).
							WithRequestRedirectScheme("http").
							WithRequestRedirectHost("example.com").Build(),
					},
				},
			}
			_, err = gatewayClient.GatewayV1().HTTPRoutes(namespace).Update(ctx, httpRoute, metav1.UpdateOptions{})
			assert.NoError(t, err)
			t.Logf("Verify that no path modifier preserves path in the request")
			assert.Eventually(
				t, func() bool {
					resp, err := httpClientIgnoreRedirect.Get(proxyHTTPURL.String() + "/redirect-target")
					if err != nil {
						return false
					}
					defer resp.Body.Close()
					return resp.StatusCode == http.StatusMovedPermanently &&
						resp.Header.Get("Location") == "http://example.com/redirect-target"
				},
				consts.IngressWait, consts.WaitTick,
			)

			return ctx
		}).Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}

// kongVersionFromProxy returns the version of Kong Gateway serving the given proxy URL, as reported
// in the Server header (e.g. "kong/3.9.1") of the responses it generates. The Admin API of DataPlanes
// managed by the operator is only reachable with the client certificate of their ControlPlane.
func kongVersionFromProxy(ctx context.Context, t *testing.T, proxyURL *url.URL) kong.Version {
	t.Helper()

	var version kong.Version
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL.String()+"/kong-version-check", nil)
		if !assert.NoError(c, err) {
			return
		}
		// Do not follow redirects: the route under test redirects every request to an external host,
		// whose Server header would be reported instead of Kong's.
		client := helpers.DefaultHTTPClient()
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		resp, err := client.Do(req)
		if !assert.NoError(c, err) {
			return
		}
		defer resp.Body.Close()

		server, ok := strings.CutPrefix(resp.Header.Get("Server"), "kong/")
		if !assert.Truef(c, ok, "unexpected Server header %q", resp.Header.Get("Server")) {
			return
		}
		version, err = kong.ParseSemanticVersion(server)
		assert.NoError(c, err)
	}, consts.IngressWait, consts.WaitTick)
	return version
}
