package ops

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

type recordedRequest struct {
	URL  *url.URL
	body []byte
}

// recordingTransport records every request and always answers 404 so the ops
// return early instead of needing a live Konnect.
type recordingTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}

	rt.mu.Lock()
	rt.requests = append(rt.requests, recordedRequest{URL: req.URL, body: body})
	rt.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

// newRecordingSDK builds an SDK against a regional server, so a call that
// forgets GlobalURL() lands on the regional host and fails the test.
func newRecordingSDK(t *testing.T, serverURL string) (sdkops.SDKWrapper, *recordingTransport) {
	t.Helper()

	rt := &recordingTransport{}
	srv, err := server.NewServer[konnectv1alpha2.KonnectGatewayControlPlane](serverURL)
	require.NoError(t, err)

	sdk, err := sdkops.NewSDKFactory(sdkops.WithHTTPClient(&http.Client{Transport: rt})).NewKonnectSDK(srv, "token")
	require.NoError(t, err)
	return sdk, rt
}

type globalURLCall struct {
	name string
	call func(ctx context.Context, sdk sdkops.SDKWrapper) error
}

// globalURLCalls holds one entry per Cloud Gateways op.
var globalURLCalls = []globalURLCall{
	{
		name: "network create",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return createKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testNetwork())
		},
	},
	{
		name: "network update",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return updateKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testNetwork())
		},
	},
	{
		name: "network delete",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return deleteKonnectNetwork(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testNetwork())
		},
	},
	{
		name: "network get matching spec name",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			_, err := getKonnectNetworkMatchingSpecName(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testNetwork())
			return err
		},
	},
	{
		name: "network adopt",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return adoptKonnectCloudGatewayNetwork(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testNetwork(), commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Mode:    commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "net-1"},
			})
		},
	},
	{
		name: "transit gateway create",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return createKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testTransitGateway())
		},
	},
	{
		name: "transit gateway update",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return updateKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testTransitGateway())
		},
	},
	{
		name: "transit gateway delete",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return deleteKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testTransitGateway())
		},
	},
	{
		name: "transit gateway get matching spec name",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			_, err := getKonnectTransitGatewayMatchingSpecName(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testTransitGateway())
			return err
		},
	},
	{
		name: "transit gateway adopt",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return adoptKonnectTransitGateway(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(), testTransitGateway(), commonv1alpha1.AdoptOptions{
				From:    commonv1alpha1.AdoptSourceKonnect,
				Mode:    commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "tg-1"},
			})
		},
	},
	{
		name: "data plane group configuration create",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return createKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(),
				testClient(), testDataPlaneGroupConfiguration())
		},
	},
	{
		name: "data plane group configuration update",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return updateKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(),
				testClient(), testDataPlaneGroupConfiguration())
		},
	},
	{
		name: "data plane group configuration delete",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return deleteKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(),
				testDataPlaneGroupConfiguration())
		},
	},
	{
		name: "data plane group configuration adopt",
		call: func(ctx context.Context, sdk sdkops.SDKWrapper) error {
			return adoptKonnectDataPlaneGroupConfiguration(ctx, sdk.GetCloudGatewaysSDK(), sdk.GetServer(),
				testClient(), testDataPlaneGroupConfiguration(), commonv1alpha1.AdoptOptions{
					From:    commonv1alpha1.AdoptSourceKonnect,
					Mode:    commonv1alpha1.AdoptModeMatch,
					Konnect: &commonv1alpha1.AdoptKonnectOptions{ID: "cfg-1"},
				})
		},
	},
}

func testNetwork() *konnectv1alpha1.KonnectCloudGatewayNetwork {
	n := &konnectv1alpha1.KonnectCloudGatewayNetwork{}
	n.Spec.Name = "test-network"
	n.SetKonnectID("net-1")
	return n
}

func testTransitGateway() *konnectv1alpha1.KonnectCloudGatewayTransitGateway {
	tg := &konnectv1alpha1.KonnectCloudGatewayTransitGateway{}
	tg.Status.NetworkID = "net-1"
	tg.SetKonnectID("tg-1")
	tg.Spec.Type = konnectv1alpha1.TransitGatewayTypeAWSTransitGateway
	tg.Spec.AWSTransitGateway = &konnectv1alpha1.AWSTransitGateway{Name: "test-tg"}
	return tg
}

func testDataPlaneGroupConfiguration() *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration {
	cfg := &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{}
	cfg.Status.ControlPlaneID = "cp-1"
	cfg.SetKonnectID("cfg-1")
	return cfg
}

// testClient is an empty fake client: the specs used here resolve no references.
func testClient() client.Client {
	return fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
}

func TestCloudGatewaysOpsUseGlobalServerURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		serverURL      string
		wantHost       string
		wantPathPrefix string
	}{
		{serverURL: "https://us.svc.konghq.com/api", wantHost: "global.svc.konghq.com", wantPathPrefix: "/api/v2/cloud-gateways/"},
		{serverURL: "https://us.api.konghq.com", wantHost: "global.api.konghq.com", wantPathPrefix: "/v2/cloud-gateways/"},
	} {
		t.Run(tc.serverURL, func(t *testing.T) {
			t.Parallel()

			for _, call := range globalURLCalls {
				t.Run(call.name, func(t *testing.T) {
					t.Parallel()

					sdk, rt := newRecordingSDK(t, tc.serverURL)
					// Errors are expected: the transport answers 404.
					_ = call.call(t.Context(), sdk)

					require.NotEmpty(t, rt.requests)
					for _, r := range rt.requests {
						assert.Equal(t, tc.wantHost, r.URL.Host)
						assert.True(t, strings.HasPrefix(r.URL.Path, tc.wantPathPrefix), r.URL.Path)
					}
				})
			}
		})
	}
}

// TestCreateDataPlaneGroupConfigurationKeepsRegionalGeo makes sure the call goes
// to the global host while control_plane_geo still comes from the regional server.
func TestCreateDataPlaneGroupConfigurationKeepsRegionalGeo(t *testing.T) {
	t.Parallel()

	sdk, rt := newRecordingSDK(t, "https://us.svc.konghq.com/api")

	// Errors are expected: the transport answers 404.
	_ = createKonnectDataPlaneGroupConfiguration(t.Context(), sdk.GetCloudGatewaysSDK(), sdk.GetServer(),
		testClient(), testDataPlaneGroupConfiguration())

	require.NotEmpty(t, rt.requests)
	assert.Contains(t, string(rt.requests[0].body), `"control_plane_geo":"us"`)
	assert.Equal(t, "global.svc.konghq.com", rt.requests[0].URL.Host)
}
