package sdk

import (
	"context"
	"reflect"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/retry"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

// supportedOptions are the per-call options the Cloud Gateways endpoints accept.
var supportedOptions = []string{
	sdkkonnectops.SupportedOptionRetries,
	sdkkonnectops.SupportedOptionTimeout,
	sdkkonnectops.SupportedOptionAcceptHeaderOverride,
	sdkkonnectops.SupportedOptionURLOverride,
}

// applyOptions applies opts the way the SDK does, so their effect is observable.
func applyOptions(t *testing.T, opts ...sdkkonnectops.Option) sdkkonnectops.Options {
	t.Helper()

	var applied sdkkonnectops.Options
	for _, opt := range opts {
		require.NoError(t, opt(&applied, supportedOptions...))
	}
	return applied
}

// TestNewGlobalCloudGatewaysSDKAppendsGlobalServerURL checks every wrapped method
// forwards the caller's options and appends the global server URL as the last one.
func TestNewGlobalCloudGatewaysSDKAppendsGlobalServerURL(t *testing.T) {
	t.Parallel()

	srv := lo.Must(server.NewServer[any]("https://us.svc.konghq.com/api"))
	callerOpt := sdkkonnectops.WithRetries(retry.Config{})
	calls := []struct {
		name string
		// record sets the mock expectation and records the options the SDK received.
		record func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments))
		call   func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option)
	}{
		{
			name: "CreateNetwork",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("CreateNetwork", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.CreateNetwork(ctx, sdkkonnectcomp.CreateNetworkRequest{}, opts...)
			},
		},
		{
			name: "GetNetwork",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("GetNetwork", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.GetNetwork(ctx, "net-1", opts...)
			},
		},
		{
			name: "DeleteNetwork",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("DeleteNetwork", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.DeleteNetwork(ctx, "net-1", opts...)
			},
		},
		{
			name: "ListNetworks",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("ListNetworks", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.ListNetworks(ctx, sdkkonnectops.ListNetworksRequest{}, opts...)
			},
		},
		{
			name: "CreateConfiguration",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("CreateConfiguration", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.CreateConfiguration(ctx, sdkkonnectcomp.CreateConfigurationRequest{}, opts...)
			},
		},
		{
			name: "GetConfiguration",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("GetConfiguration", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.GetConfiguration(ctx, "cfg-1", opts...)
			},
		},
		{
			name: "CreateTransitGateway",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("CreateTransitGateway", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.CreateTransitGateway(ctx, "net-1", sdkkonnectcomp.CreateTransitGatewayRequest{}, opts...)
			},
		},
		{
			name: "GetTransitGateway",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("GetTransitGateway", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.GetTransitGateway(ctx, "net-1", "tg-1", opts...)
			},
		},
		{
			name: "DeleteTransitGateway",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("DeleteTransitGateway", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.DeleteTransitGateway(ctx, "net-1", "tg-1", opts...)
			},
		},
		{
			name: "ListTransitGateways",
			record: func(cg *mocks.MockCloudGatewaysSDK, capture func(mock.Arguments)) {
				cg.On("ListTransitGateways", mock.Anything, mock.Anything, mock.Anything).Run(capture).Return(nil, nil)
			},
			call: func(ctx context.Context, sdk CloudGatewaysSDK, opts ...sdkkonnectops.Option) {
				_, _ = sdk.ListTransitGateways(ctx, sdkkonnectops.ListTransitGatewaysRequest{}, opts...)
			},
		},
	}
	require.Len(t, calls, reflect.TypeFor[CloudGatewaysSDK]().NumMethod(),
		"every CloudGatewaysSDK method needs a row, or it can silently skip the global URL")

	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got mock.Arguments
			cg := mocks.NewMockCloudGatewaysSDK(t)
			tc.record(cg, func(args mock.Arguments) { got = args })

			tc.call(t.Context(), NewGlobalCloudGatewaysSDK(cg, srv), callerOpt)

			require.NotNil(t, got)
			forwarded, ok := got.Get(len(got) - 1).([]sdkkonnectops.Option)
			require.True(t, ok, "the SDK method must be called with options")

			// The caller's option, then the global server URL appended by the wrapper.
			require.Len(t, forwarded, 2)
			applied := applyOptions(t, forwarded...)
			assert.NotNil(t, applied.Retries)
			require.NotNil(t, applied.ServerURL)
			assert.Equal(t, srv.GlobalURL(), *applied.ServerURL)

			// The global URL is last, so a caller-supplied URL cannot win.
			last := applyOptions(t, forwarded[len(forwarded)-1])
			require.NotNil(t, last.ServerURL)
			assert.Equal(t, srv.GlobalURL(), *last.ServerURL)
		})
	}
}
