package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ConsumersSDK is the interface for the Konnect Consumers SDK.
type ConsumersSDK interface {
	CreateConsumer(ctx context.Context, controlPlaneID string, consumerInput sdkkonnectcomp.Consumer, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateConsumerResponse, error)
	UpsertConsumer(ctx context.Context, upsertConsumerRequest sdkkonnectops.UpsertConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertConsumerResponse, error)
	DeleteConsumer(ctx context.Context, controlPlaneID string, consumerID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteConsumerResponse, error)
	ListConsumer(ctx context.Context, request sdkkonnectops.ListConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListConsumerResponse, error)
	ListConsumerGroupsForConsumer(ctx context.Context, request sdkkonnectops.ListConsumerGroupsForConsumerRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListConsumerGroupsForConsumerResponse, error)
}
