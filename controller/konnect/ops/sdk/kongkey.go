package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KeysSDK is the interface for the KeysSDK.
type KeysSDK interface {
	CreateKey(ctx context.Context, controlPlaneID string, Key sdkkonnectcomp.Key, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateKeyResponse, error)
	UpsertKey(ctx context.Context, request sdkkonnectops.UpsertKeyRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertKeyResponse, error)
	DeleteKey(ctx context.Context, controlPlaneID string, KeyID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteKeyResponse, error)
	GetKey(ctx context.Context, keyID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetKeyResponse, error)
	ListKey(ctx context.Context, request sdkkonnectops.ListKeyRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListKeyResponse, error)
}
