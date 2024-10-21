package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ControlPlaneSDK is the interface for the Konnect ControlPlane SDK.
type ControlPlaneSDK interface {
	CreateControlPlane(ctx context.Context, req sdkkonnectcomp.CreateControlPlaneRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateControlPlaneResponse, error)
	DeleteControlPlane(ctx context.Context, id string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteControlPlaneResponse, error)
	UpdateControlPlane(ctx context.Context, id string, req sdkkonnectcomp.UpdateControlPlaneRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpdateControlPlaneResponse, error)
	ListControlPlanes(ctx context.Context, request sdkkonnectops.ListControlPlanesRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListControlPlanesResponse, error)
}
