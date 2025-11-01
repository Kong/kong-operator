package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KongCredentialJWTSDK is the interface for the Konnect KongCredentialJWTSDK.
type KongCredentialJWTSDK interface {
	CreateJwtWithConsumer(ctx context.Context, req sdkkonnectops.CreateJwtWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateJwtWithConsumerResponse, error)
	DeleteJwtWithConsumer(ctx context.Context, request sdkkonnectops.DeleteJwtWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteJwtWithConsumerResponse, error)
	UpsertJwtWithConsumer(ctx context.Context, request sdkkonnectops.UpsertJwtWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertJwtWithConsumerResponse, error)
	GetJwtWithConsumer(ctx context.Context, request sdkkonnectops.GetJwtWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetJwtWithConsumerResponse, error)
	ListJwt(ctx context.Context, request sdkkonnectops.ListJwtRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListJwtResponse, error)
}
