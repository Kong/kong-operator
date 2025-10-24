package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KongCredentialBasicAuthSDK is the interface for the Konnect KongCredentialBasicAuthSDK.
type KongCredentialBasicAuthSDK interface {
	CreateBasicAuthWithConsumer(ctx context.Context, req sdkkonnectops.CreateBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateBasicAuthWithConsumerResponse, error)
	DeleteBasicAuthWithConsumer(ctx context.Context, request sdkkonnectops.DeleteBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteBasicAuthWithConsumerResponse, error)
	UpsertBasicAuthWithConsumer(ctx context.Context, request sdkkonnectops.UpsertBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertBasicAuthWithConsumerResponse, error)
	GetBasicAuthWithConsumer(ctx context.Context, request sdkkonnectops.GetBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetBasicAuthWithConsumerResponse, error)
	ListBasicAuth(ctx context.Context, request sdkkonnectops.ListBasicAuthRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListBasicAuthResponse, error)
}
