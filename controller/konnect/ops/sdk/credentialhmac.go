package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KongCredentialHMACSDK is the interface for the Konnect KongCredentialHMACSDK.
type KongCredentialHMACSDK interface {
	CreateHmacAuthWithConsumer(ctx context.Context, req sdkkonnectops.CreateHmacAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateHmacAuthWithConsumerResponse, error)
	DeleteHmacAuthWithConsumer(ctx context.Context, request sdkkonnectops.DeleteHmacAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteHmacAuthWithConsumerResponse, error)
	UpsertHmacAuthWithConsumer(ctx context.Context, request sdkkonnectops.UpsertHmacAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertHmacAuthWithConsumerResponse, error)
	GetHmacAuthWithConsumer(ctx context.Context, request sdkkonnectops.GetHmacAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetHmacAuthWithConsumerResponse, error)
	ListHmacAuth(ctx context.Context, request sdkkonnectops.ListHmacAuthRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListHmacAuthResponse, error)
}
