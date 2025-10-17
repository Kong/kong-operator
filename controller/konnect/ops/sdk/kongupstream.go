package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// UpstreamsSDK is the interface for the Konnect Upstream SDK.
type UpstreamsSDK interface {
	CreateUpstream(ctx context.Context, controlPlaneID string, upstream sdkkonnectcomp.Upstream, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateUpstreamResponse, error)
	UpsertUpstream(ctx context.Context, req sdkkonnectops.UpsertUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertUpstreamResponse, error)
	GetUpstream(ctx context.Context, upstreamID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetUpstreamResponse, error)
	DeleteUpstream(ctx context.Context, controlPlaneID, upstreamID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteUpstreamResponse, error)
	ListUpstream(ctx context.Context, request sdkkonnectops.ListUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListUpstreamResponse, error)
}
