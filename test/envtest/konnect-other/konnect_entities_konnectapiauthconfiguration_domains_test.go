package konnectother

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
)

// roundTripFunc fails the test when a request is made: a server URL the operator
// must not use has to be refused before the SDK reaches the network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestKonnectAPIAuthConfigurationServerURLDomains(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with the real SDK factory")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkops.NewSDKFactory(
		sdkops.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("the operator sent a request to an unusable server URL")
			return nil, errors.New("no request expected")
		})}),
	)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(controller.Options{}, factory, logging.DevelopmentMode, mgr.GetClient()),
	)

	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	for _, tc := range []struct {
		name           string
		serverURL      string
		wantMessage    string
		wantParseError bool
	}{
		{
			name:        "host outside the allowed domains",
			serverURL:   "https://us.attacker.example",
			wantMessage: "konghq.com",
		},
		{
			name:           "host whose region does not parse",
			serverURL:      "fooo.api.konghq.com",
			wantMessage:    "failed to parse server URL",
			wantParseError: true,
		},
	} {
		t.Run("gets APIAuthValid=false "+tc.name, func(t *testing.T) {
			w := envtest.SetupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
			apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced, func(obj client.Object) {
				obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration).Spec.ServerURL = tc.serverURL
			})
			t.Cleanup(func() { require.NoError(t, cl.Delete(ctx, apiAuth)) })

			envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
				if client.ObjectKeyFromObject(r) != client.ObjectKeyFromObject(apiAuth) {
					return false
				}
				cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, r)
				return ok &&
					cond.Status == metav1.ConditionFalse &&
					cond.Reason == konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid &&
					strings.Contains(cond.Message, tc.wantMessage)
			}, "KonnectAPIAuthConfiguration didn't get APIAuthValid=false with a message naming the reason")

			require.Empty(t, apiAuth.Status.OrganizationID)
		})
	}
}
