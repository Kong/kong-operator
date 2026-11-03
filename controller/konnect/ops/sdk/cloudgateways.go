package sdk

import (
	"context"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

// CloudGatewaysSDK are the Cloud Gateways methods the operator uses; the SDK
// ignores the client-level server URL for them, so get one from NewGlobalCloudGatewaysSDK.
type CloudGatewaysSDK interface {
	CreateNetwork(ctx context.Context, request sdkkonnectcomp.CreateNetworkRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateNetworkResponse, error)
	GetNetwork(ctx context.Context, networkID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetNetworkResponse, error)
	DeleteNetwork(ctx context.Context, networkID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteNetworkResponse, error)
	ListNetworks(ctx context.Context, request sdkkonnectops.ListNetworksRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListNetworksResponse, error)
	CreateConfiguration(ctx context.Context, request sdkkonnectcomp.CreateConfigurationRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateConfigurationResponse, error)
	GetConfiguration(ctx context.Context, configurationID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetConfigurationResponse, error)
	CreateTransitGateway(ctx context.Context, networkID string, request sdkkonnectcomp.CreateTransitGatewayRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateTransitGatewayResponse, error)
	GetTransitGateway(ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetTransitGatewayResponse, error)
	DeleteTransitGateway(ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteTransitGatewayResponse, error)
	ListTransitGateways(ctx context.Context, request sdkkonnectops.ListTransitGatewaysRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListTransitGatewaysResponse, error)
}

// NewGlobalCloudGatewaysSDK returns cg with every call forced to the global form
// of srv's URL, so no Cloud Gateways call can land on a regional host.
func NewGlobalCloudGatewaysSDK(cg sdkkonnectgo.CloudGatewaysSDK, srv server.Server) CloudGatewaysSDK {
	return globalCloudGatewaysSDK{cg: cg, globalURL: srv.GlobalURL()}
}

type globalCloudGatewaysSDK struct {
	cg        sdkkonnectgo.CloudGatewaysSDK
	globalURL string
}

var _ CloudGatewaysSDK = globalCloudGatewaysSDK{}

// opts copies the caller's options before appending, so a caller slice is never
// mutated and its own server URL override cannot win.
func (g globalCloudGatewaysSDK) opts(opts []sdkkonnectops.Option) []sdkkonnectops.Option {
	return append(slices.Clone(opts), sdkkonnectops.WithServerURL(g.globalURL))
}

// CreateNetwork implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) CreateNetwork(
	ctx context.Context, request sdkkonnectcomp.CreateNetworkRequest, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.CreateNetworkResponse, error) {
	return g.cg.CreateNetwork(ctx, request, g.opts(opts)...)
}

// GetNetwork implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) GetNetwork(
	ctx context.Context, networkID string, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.GetNetworkResponse, error) {
	return g.cg.GetNetwork(ctx, networkID, g.opts(opts)...)
}

// DeleteNetwork implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) DeleteNetwork(
	ctx context.Context, networkID string, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.DeleteNetworkResponse, error) {
	return g.cg.DeleteNetwork(ctx, networkID, g.opts(opts)...)
}

// ListNetworks implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) ListNetworks(
	ctx context.Context, request sdkkonnectops.ListNetworksRequest, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.ListNetworksResponse, error) {
	return g.cg.ListNetworks(ctx, request, g.opts(opts)...)
}

// CreateConfiguration implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) CreateConfiguration(
	ctx context.Context, request sdkkonnectcomp.CreateConfigurationRequest, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.CreateConfigurationResponse, error) {
	return g.cg.CreateConfiguration(ctx, request, g.opts(opts)...)
}

// GetConfiguration implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) GetConfiguration(
	ctx context.Context, configurationID string, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.GetConfigurationResponse, error) {
	return g.cg.GetConfiguration(ctx, configurationID, g.opts(opts)...)
}

// CreateTransitGateway implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) CreateTransitGateway(
	ctx context.Context, networkID string, request sdkkonnectcomp.CreateTransitGatewayRequest, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.CreateTransitGatewayResponse, error) {
	return g.cg.CreateTransitGateway(ctx, networkID, request, g.opts(opts)...)
}

// GetTransitGateway implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) GetTransitGateway(
	ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.GetTransitGatewayResponse, error) {
	return g.cg.GetTransitGateway(ctx, networkID, transitGatewayID, g.opts(opts)...)
}

// DeleteTransitGateway implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) DeleteTransitGateway(
	ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.DeleteTransitGatewayResponse, error) {
	return g.cg.DeleteTransitGateway(ctx, networkID, transitGatewayID, g.opts(opts)...)
}

// ListTransitGateways implements CloudGatewaysSDK.
func (g globalCloudGatewaysSDK) ListTransitGateways(
	ctx context.Context, request sdkkonnectops.ListTransitGatewaysRequest, opts ...sdkkonnectops.Option,
) (*sdkkonnectops.ListTransitGatewaysResponse, error) {
	return g.cg.ListTransitGateways(ctx, request, g.opts(opts)...)
}
