package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// CloudGatewaysSDK is the interface for the Konnect Dedicated Cloud Gateways SDK.
type CloudGatewaysSDK interface {
	CreateNetwork(ctx context.Context, request sdkkonnectcomp.CreateNetworkRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateNetworkResponse, error)
	GetNetwork(ctx context.Context, networkID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetNetworkResponse, error)
	ListNetworks(ctx context.Context, request sdkkonnectops.ListNetworksRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListNetworksResponse, error)
	UpdateNetwork(ctx context.Context, networkID string, patchNetworkRequest sdkkonnectcomp.PatchNetworkRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpdateNetworkResponse, error)
	DeleteNetwork(ctx context.Context, networkID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteNetworkResponse, error)

	CreateConfiguration(ctx context.Context, request sdkkonnectcomp.CreateConfigurationRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateConfigurationResponse, error)
	GetConfiguration(ctx context.Context, configurationID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetConfigurationResponse, error)
	ListConfigurations(ctx context.Context, request sdkkonnectops.ListConfigurationsRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListConfigurationsResponse, error)
}
