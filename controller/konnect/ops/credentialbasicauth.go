package ops

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// CredentialBasicAuthSDK is the interface for the Konnect CredentialBasicAuthSDK.
type CredentialBasicAuthSDK interface {
	CreateBasicAuthWithConsumer(ctx context.Context, req sdkkonnectops.CreateBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateBasicAuthWithConsumerResponse, error)
	DeleteBasicAuthWithConsumer(ctx context.Context, request sdkkonnectops.DeleteBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteBasicAuthWithConsumerResponse, error)
	UpsertBasicAuthWithConsumer(ctx context.Context, request sdkkonnectops.UpsertBasicAuthWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertBasicAuthWithConsumerResponse, error)
}
