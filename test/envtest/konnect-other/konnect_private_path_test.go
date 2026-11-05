package konnectother

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

// konnectPrivateToken is the token the fake expects to see. It must start with
// kpat_ to pass the CRD's own validation.
const konnectPrivateToken = "kpat_privatepathfaketoken"

// TestKonnectPrivatePath drives the operator's real Konnect client against a
// fake Konnect API served on the private hostnames. Nothing is mocked below the
// SDK: the hostname, the path prefix and the certificate are the ones a
// customer behind PrivateLink gets, so a change that sends those calls
// elsewhere, or drops the path, fails here.
func TestKonnectPrivatePath(t *testing.T) {
	t.Parallel()

	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	fake := envtest.StartKonnectPrivateServer(t)
	factory := sdkops.NewSDKFactory(sdkops.WithHTTPClient(fake.HTTPClient()))

	t.Log("Setting up the manager with the real SDK factory")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(controller.Options{}, factory, logging.DevelopmentMode, mgr.GetClient()),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha2.KonnectGatewayControlPlane](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha2.KonnectGatewayControlPlane](&metricsmocks.MockRecorder{}),
		),
	)

	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Run("authenticates and reconciles a control plane over the private host", func(t *testing.T) {
		authWatch := envtest.SetupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
			deploy.KonnectAPIAuthConfigurationWithTestToken(konnectPrivateToken, envtest.KonnectPrivateServerURL))

		t.Log("Waiting for the authentication to resolve the organization")
		envtest.WatchFor(t, ctx, authWatch, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, r) &&
				r.Status.OrganizationID == envtest.KonnectPrivateOrgID
		}, "KonnectAPIAuthConfiguration did not become valid with the fake organization ID")

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(apiAuth), apiAuth))
		assert.Equal(t, envtest.KonnectPrivateServerURL, apiAuth.Status.ServerURL,
			"the private serverURL must be kept as configured")

		cpWatch := envtest.SetupWatch[konnectv1alpha2.KonnectGatewayControlPlaneList](t, ctx, cl, client.InNamespace(ns.Name))
		cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, apiAuth)

		t.Log("Waiting for the control plane to be programmed with the private endpoints")
		// The endpoints are filled on the reconcile after the create: wait for
		// them rather than reading the object as soon as it is programmed.
		envtest.WatchFor(t, ctx, cpWatch, apiwatch.Modified, func(r *konnectv1alpha2.KonnectGatewayControlPlane) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(cp) &&
				envtest.ConditionsContainProgrammed(r.GetConditions(), metav1.ConditionTrue) &&
				r.GetKonnectID() == envtest.KonnectPrivateControlPlaneID &&
				r.Status.Endpoints != nil &&
				r.Status.Endpoints.ControlPlaneEndpoint == envtest.KonnectPrivateControlPlaneEndpoint &&
				r.Status.Endpoints.TelemetryEndpoint == envtest.KonnectPrivateTelemetryEndpoint
		}, "KonnectGatewayControlPlane did not get programmed with the fake Konnect ID and the private endpoints")

		t.Log("Checking the requests the fake received")
		requests := fake.Requests()
		require.NotEmpty(t, requests)

		assert.Contains(t, requests, envtest.KonnectRequest{
			Method:        http.MethodGet,
			Host:          envtest.KonnectPrivateRegionalHost,
			ServerName:    envtest.KonnectPrivateRegionalHost,
			Path:          "/api/v3/organizations/me",
			Authorization: "Bearer " + konnectPrivateToken,
		}, "the identity call must go to the private host, carrying the path and the token")

		assert.Contains(t, requests, envtest.KonnectRequest{
			Method:        http.MethodPost,
			Host:          envtest.KonnectPrivateRegionalHost,
			ServerName:    envtest.KonnectPrivateRegionalHost,
			Path:          "/api/v2/control-planes",
			Authorization: "Bearer " + konnectPrivateToken,
		}, "the control plane create must go to the private host's path, authenticated")

		privateHosts := []string{envtest.KonnectPrivateRegionalHost, envtest.KonnectPrivateGlobalHost}
		for _, r := range requests {
			assert.Contains(t, privateHosts, r.Host, "every request must go to a private host")
			assert.Equal(t, r.Host, r.ServerName, "the TLS handshake must present the host being called")
			assert.Contains(t, r.Path, "/api/", "the configured path prefix must be preserved")
		}

		t.Log("Checking the fake cannot stand in for a public host")
		_, err := fake.HTTPClient().Get("https://global.api.konghq.com/v3/organizations/me")
		require.Error(t, err, "a call to a host outside the private endpoints must fail")
		var certErr *tls.CertificateVerificationError
		assert.ErrorAs(t, err, &certErr, "the failure must be the certificate verification")

		t.Log("Deleting the control plane")
		require.NoError(t, cl.Delete(ctx, cp))
		require.Eventually(t, func() bool {
			err := cl.Get(ctx, client.ObjectKeyFromObject(cp), cp)
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 100*time.Millisecond, "the control plane must be deleted")
	})
}
