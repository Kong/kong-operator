package sdk

import (
	"context"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// KongCredentialACLSDK is the interface for the Konnect KongCredentialACLSDK.
type KongCredentialACLSDK interface {
	CreateACLWithConsumer(ctx context.Context, req sdkkonnectops.CreateACLWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateACLWithConsumerResponse, error)
	DeleteACLWithConsumer(ctx context.Context, request sdkkonnectops.DeleteACLWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteACLWithConsumerResponse, error)
	UpsertACLWithConsumer(ctx context.Context, request sdkkonnectops.UpsertACLWithConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertACLWithConsumerResponse, error)
}
