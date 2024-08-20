package ops

import (
	"context"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
)

// RoutesSDK is the interface for the Konnect Routes SDK.
type RoutesSDK interface {
	CreateRoute(ctx context.Context, controlPlaneID string, route sdkkonnectgocomp.RouteInput, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateRouteResponse, error)
	UpsertRoute(ctx context.Context, req sdkkonnectgoops.UpsertRouteRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpsertRouteResponse, error)
	DeleteRoute(ctx context.Context, controlPlaneID, routeID string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteRouteResponse, error)
}
