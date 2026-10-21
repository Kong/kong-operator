package isolated

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/diagnostics"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

func TestIngressWithBrokenPluginFallback(t *testing.T) {
	ingressWithBrokenPluginFallback := samplesManifestPath("feature-gated/ingress-broken-plugin-fallback.yaml")

	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyIngress).
		WithLabel(testlabels.Kind, testlabels.KindIngress).
		WithSetup("skip as the broken KongPlugin is rejected by the webhook", SkipBrokenKongPluginsRejectedByWebhook).
		WithSetup("create test namespace", featureSetup(withoutGateway())).
		Assess("deploying to cluster works and HTTP traffic is routed to the service",
			func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
				// The sample brings its own IngressClass and a standalone DataPlane and ControlPlane, with the
				// FallbackConfiguration feature gate enabled. Use the test's ingress class instead of the
				// cluster-scoped `kong` one and enable config dumps to verify the fallback via the diagnostics server.
				ingressClass := GetIngressClassFromCtx(ctx)
				applyManifest(ctx, t, ingressWithBrokenPluginFallback,
					"ingressClassName: kong", "ingressClassName: "+ingressClass,
					"ingressClass: kong", "ingressClass: "+ingressClass,
					"name: kong\nspec:\n  controller:", "name: "+ingressClass+"-sample\nspec:\n  controller:",
					"  featureGates:\n", "  configDump:\n    state: enabled\n  featureGates:\n",
				)
				namespace := GetNamespaceForT(ctx, t)
				ctx = waitForDataPlane(ctx, t, k8stypes.NamespacedName{Namespace: namespace, Name: "dataplane-fallback"})
				ctx = SetInCtxForT(ctx, t, controlPlaneRef{Namespace: namespace, Name: "controlplane-fallback"})
				proxyURL := GetHTTPSURLFromCtx(ctx)

				t.Logf("verifying that the Ingress routes traffic properly to the /ingress-testing path")
				helpers.EventuallyGETPath(
					t,
					proxyURL,
					proxyURL.String(),
					"/ingress-testing",
					&helpers.HTTPSOptions{
						InsecureSkipVerify: true,
					},
					http.StatusOK,
					"Running on Pod",
					nil,
					consts.IngressWait,
					consts.WaitTick,
				)

				return ctx
			}).
		Assess("verify that valid Ingress's status contains LoadBalancer information", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			cluster := GetClusterFromCtx(ctx)
			// Verify Ingress status contains LoadBalancer information
			require.Eventually(t, func() bool {
				ingress, err := cluster.Client().NetworkingV1().Ingresses(GetNamespaceForT(ctx, t)).Get(ctx, "working-ingress", metav1.GetOptions{})
				if err != nil {
					t.Logf("Failed to get Ingress: %v", err)
					return false
				}
				lbStatus := ingress.Status.LoadBalancer.Ingress
				return len(lbStatus) > 0
			}, consts.IngressWait, consts.WaitTick)
			return ctx
		}).
		Assess("verify that route with misconfigured plugin is not operational", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			proxyURL := GetHTTPSURLFromCtx(ctx)
			t.Logf("verifying that Kong gateway response is returned instead of desired site")

			helpers.EventuallyGETPath(
				t,
				proxyURL,
				proxyURL.String(),
				"/for-auth-users",
				&helpers.HTTPSOptions{
					InsecureSkipVerify: true,
				},
				http.StatusNotFound,
				"no Route matched with those values",
				nil,
				consts.IngressWait,
				consts.WaitTick,
			)
			return ctx
		}).
		Assess("verify that invalid Ingress's status doesn't contain LoadBalancer information", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			cluster := GetClusterFromCtx(ctx)
			// Verify Ingress status contains LoadBalancer information
			require.Eventually(t, func() bool {
				ingress, err := cluster.Client().NetworkingV1().Ingresses(GetNamespaceForT(ctx, t)).Get(ctx, "for-auth", metav1.GetOptions{})
				if err != nil {
					t.Logf("Failed to get Ingress: %v", err)
					return false
				}
				lbStatus := ingress.Status.LoadBalancer.Ingress
				return len(lbStatus) == 0
			}, consts.IngressWait, consts.WaitTick)
			return ctx
		}).
		Assess("verify diagnostic server fallback info", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			diagURL := GetDiagURLFromCtx(ctx)
			fallbackPath := controlPlaneDiagnosticsPath(ctx, t, "/fallback")
			t.Logf("verifying diag available")
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				cl := helpers.DefaultHTTPClient()
				resp, err := cl.Do(helpers.MustHTTPRequest(t, http.MethodGet, diagURL.Host, fallbackPath, nil))
				if !assert.NoError(c, err) {
					return
				}
				defer resp.Body.Close()

				if !assert.Equal(c, http.StatusOK, resp.StatusCode) {
					return
				}

				response := diagnostics.FallbackResponse{}
				err = json.NewDecoder(resp.Body).Decode(&response)
				if !assert.NoError(c, err) {
					return
				}
				assert.Equal(c, diagnostics.FallbackStatusTriggered, response.Status)
				assert.NotEmpty(c, response.ExcludedObjects)
				contains := lo.ContainsBy(response.ExcludedObjects, func(obj diagnostics.FallbackAffectedObjectMeta) bool {
					return obj.Group == "configuration.konghq.com" && obj.Kind == "KongPlugin" && obj.Name == "key-auth"
				})
				assert.Truef(c, contains, "expected to find KongPlugin key-auth in excluded objects, got: %v", response.ExcludedObjects)
			}, consts.IngressWait, consts.WaitTick)
			return ctx
		}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
