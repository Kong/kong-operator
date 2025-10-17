package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// TargetsSDK is the interface for the Konnect Taret SDK.
type TargetsSDK interface {
	CreateTargetWithUpstream(ctx context.Context, req sdkkonnectops.CreateTargetWithUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateTargetWithUpstreamResponse, error)
	UpsertTargetWithUpstream(ctx context.Context, req sdkkonnectops.UpsertTargetWithUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertTargetWithUpstreamResponse, error)
	GetTargetWithUpstream(ctx context.Context, req sdkkonnectops.GetTargetWithUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetTargetWithUpstreamResponse, error)
	DeleteTargetWithUpstream(ctx context.Context, req sdkkonnectops.DeleteTargetWithUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteTargetWithUpstreamResponse, error)
	ListTargetWithUpstream(ctx context.Context, request sdkkonnectops.ListTargetWithUpstreamRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListTargetWithUpstreamResponse, error)
}
