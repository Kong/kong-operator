package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// TransitGatewaysSDK is the interface for operating Konnect transit gateways.
// REVIEW: define the transit gateway methods as a dedicated interface or add the methods to the `CloudGatewaysSDK` interface?
type TransitGatewaysSDK interface {
	ListTransitGateways(ctx context.Context, request sdkkonnectops.ListTransitGatewaysRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListTransitGatewaysResponse, error)
	CreateTransitGateway(ctx context.Context, networkID string, createTransitGatewayRequest sdkkonnectcomp.CreateTransitGatewayRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateTransitGatewayResponse, error)
	GetTransitGateway(ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetTransitGatewayResponse, error)
	DeleteTransitGateway(ctx context.Context, networkID string, transitGatewayID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteTransitGatewayResponse, error)
}
