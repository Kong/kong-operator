package ops

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KeysSDK is the interface for the KeysSDK.
type KeysSDK interface {
	CreateKey(ctx context.Context, controlPlaneID string, caCertificate sdkkonnectcomp.KeyInput, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateKeyResponse, error)
	UpsertKey(ctx context.Context, request sdkkonnectops.UpsertKeyRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertKeyResponse, error)
	DeleteKey(ctx context.Context, controlPlaneID string, caCertificateID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteKeyResponse, error)
}
