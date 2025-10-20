package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KeySetsSDK is the interface for the KeySetsSDK.
type KeySetsSDK interface {
	CreateKeySet(ctx context.Context, controlPlaneID string, keySet *sdkkonnectcomp.KeySet, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateKeySetResponse, error)
	UpsertKeySet(ctx context.Context, request sdkkonnectops.UpsertKeySetRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertKeySetResponse, error)
	DeleteKeySet(ctx context.Context, controlPlaneID string, keySetID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteKeySetResponse, error)
	GetKeySet(ctx context.Context, keySetID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetKeySetResponse, error)
	ListKeySet(ctx context.Context, request sdkkonnectops.ListKeySetRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListKeySetResponse, error)
}
