package isolated

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/test/helpers"
)

func TestKongServiceFacadeExample(t *testing.T) {
	f := features.
		New("example").
		WithLabel(testlabels.Example, testlabels.ExampleTrue).
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyIngress).
		WithLabel(testlabels.Kind, testlabels.KindKongServiceFacade).
		WithSetup("create test namespace", featureSetup(withoutGateway())).
		WithSetup("deploy example manifest", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			// The sample brings its own IngressClass and a standalone DataPlane and ControlPlane, with the
			// KongServiceFacade feature gate enabled. Use the test's ingress class instead of the cluster-scoped
			// `kong` one.
			ingressClass := GetIngressClassFromCtx(ctx)
			t.Logf("replacing kong ingress class in yaml manifest to %s", ingressClass)
			applyManifest(ctx, t, samplesManifestPath("feature-gated/kong-service-facade.yaml"),
				"kubernetes.io/ingress.class: kong", "kubernetes.io/ingress.class: "+ingressClass,
				"ingressClassName: kong", "ingressClassName: "+ingressClass,
				"ingressClass: kong", "ingressClass: "+ingressClass,
				"name: kong\nspec:\n  controller:", "name: "+ingressClass+"-sample\nspec:\n  controller:",
			)

			namespace := GetNamespaceForT(ctx, t)
			t.Log("waiting for httpbin deployment to be ready")
			helpers.WaitForDeploymentRollout(ctx, t, GetClusterFromCtx(ctx), namespace, "httpbin-deployment")
			return waitForDataPlane(ctx, t, k8stypes.NamespacedName{Namespace: namespace, Name: "dataplane-service-facade"})
		}).
		Assess("basic-auth and key-auth plugins are applied to KongServiceFacades as expected", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			var (
				proxyURL = GetHTTPSURLFromCtx(ctx)

				keyAuthSecuredEndpoint = fmt.Sprintf("%s%s", proxyURL, "/alpha")
				keyAuthValidKey        = "alice-password"

				basicAuthSecuredEndpoint = fmt.Sprintf("%s%s", proxyURL, "/beta")
				basicAuthValidUsername   = "bob"
				basicAuthValidPassword   = "bob-password"
			)

			httpClient := helpers.DefaultHTTPClient(helpers.WithInsecureSkipVerify())
			respondsWithExpectedStatusCode := func(t *testing.T, req *http.Request, expectedStatusCode int) {
				require.Eventually(t, func() bool {
					res, err := httpClient.Do(req)
					if err != nil {
						t.Logf("%s request returned an error: %s", req.URL, err)
						return false
					}
					defer res.Body.Close()

					if res.StatusCode != expectedStatusCode {
						t.Logf("%s request returned unexpected status code %d instead of %d", req.URL, res.StatusCode, expectedStatusCode)
						return false
					}

					return true
				}, time.Minute, 250*time.Millisecond)
			}

			newRequest := func(endpoint string, modFn func(*http.Request)) *http.Request {
				req := lo.Must(http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil))
				modFn(req)
				return req
			}

			t.Run("key-auth endpoint responses", func(t *testing.T) {
				t.Log("ensuring key-auth endpoint allows a valid key")
				validKeyAuthReq := newRequest(keyAuthSecuredEndpoint, func(r *http.Request) {
					r.Header.Set("Key", keyAuthValidKey)
				})
				respondsWithExpectedStatusCode(t, validKeyAuthReq, http.StatusOK)

				t.Log("ensuring key-auth endpoint doesn't allow an invalid key")
				invalidKeyAuthReq := newRequest(keyAuthSecuredEndpoint, func(r *http.Request) {
					r.Header.Set("Key", "invalid-pass")
				})
				respondsWithExpectedStatusCode(t, invalidKeyAuthReq, http.StatusUnauthorized)

				t.Log("ensuring key-auth endpoint doesn't allow valid basic-auth credentials")
				invalidKeyAuthUsingBasicAuthReq := newRequest(keyAuthSecuredEndpoint, func(r *http.Request) {
					r.SetBasicAuth(basicAuthValidUsername, basicAuthValidPassword)
				})
				respondsWithExpectedStatusCode(t, invalidKeyAuthUsingBasicAuthReq, http.StatusUnauthorized)
			})

			t.Run("basic-auth endpoint responses", func(t *testing.T) {
				t.Log("ensuring basic-auth endpoint allows valid credentials")
				validBasicAuthReq := newRequest(basicAuthSecuredEndpoint, func(r *http.Request) {
					r.SetBasicAuth(basicAuthValidUsername, basicAuthValidPassword)
				})
				respondsWithExpectedStatusCode(t, validBasicAuthReq, http.StatusOK)

				t.Log("ensuring basic-auth endpoint doesn't allow invalid credentials")
				invalidBasicAuthReq := newRequest(basicAuthSecuredEndpoint, func(r *http.Request) {
					r.SetBasicAuth(basicAuthValidUsername, "invalid-pass")
				})
				respondsWithExpectedStatusCode(t, invalidBasicAuthReq, http.StatusUnauthorized)

				t.Log("ensuring basic-auth endpoint doesn't allow a valid key-auth key")
				invalidBasicAuthUsingKeyAuthReq := newRequest(basicAuthSecuredEndpoint, func(r *http.Request) {
					r.Header.Set("Key", keyAuthValidKey)
				})
				respondsWithExpectedStatusCode(t, invalidBasicAuthUsingKeyAuthReq, http.StatusUnauthorized)
			})

			return ctx
		}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
