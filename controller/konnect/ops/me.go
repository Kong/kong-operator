package ops

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// MeSDK is the interface for Konnect "Me" SDK to get current organization.
type MeSDK interface {
	GetOrganizationsMe(ctx context.Context, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetOrganizationsMeResponse, error)
}
