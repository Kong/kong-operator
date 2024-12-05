package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ControlPlaneGroupSDK is the interface for the Konnect ControlPlaneGroupSDK SDK.
type ControlPlaneGroupSDK interface {
	PutControlPlanesIDGroupMemberships(ctx context.Context, id string, groupMembership *sdkkonnectcomp.GroupMembership, opts ...sdkkonnectops.Option) (*sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse, error)
}
