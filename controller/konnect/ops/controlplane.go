package ops

import (
	"context"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ControlPlaneSDK is the interface for the Konnect ControlPlaneSDK SDK.
type ControlPlaneSDK interface {
	CreateControlPlane(ctx context.Context, req sdkkonnectgocomp.CreateControlPlaneRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateControlPlaneResponse, error)
	DeleteControlPlane(ctx context.Context, id string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteControlPlaneResponse, error)
	UpdateControlPlane(ctx context.Context, id string, req sdkkonnectgocomp.UpdateControlPlaneRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpdateControlPlaneResponse, error)
}
