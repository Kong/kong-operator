package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KongCredentialAPIKeySDK is the interface for the Konnect KongCredentialAPIKeySDK.
type KongCredentialAPIKeySDK interface {
	CreateKeyAuthWithConsumer(ctx context.Context, req sdkkonnectops.CreateKeyAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateKeyAuthWithConsumerResponse, error)
	DeleteKeyAuthWithConsumer(ctx context.Context, request sdkkonnectops.DeleteKeyAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteKeyAuthWithConsumerResponse, error)
	UpsertKeyAuthWithConsumer(ctx context.Context, request sdkkonnectops.UpsertKeyAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertKeyAuthWithConsumerResponse, error)
	GetKeyAuthWithConsumer(ctx context.Context, request sdkkonnectops.GetKeyAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetKeyAuthWithConsumerResponse, error)
	ListKeyAuth(ctx context.Context, request sdkkonnectops.ListKeyAuthRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListKeyAuthResponse, error)
}
