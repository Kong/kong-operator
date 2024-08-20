package ops

import (
	"context"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ConsumersSDK is the interface for the Konnect Consumers SDK.
type ConsumersSDK interface {
	CreateConsumer(ctx context.Context, controlPlaneID string, consumerInput sdkkonnectgocomp.ConsumerInput, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateConsumerResponse, error)
	UpsertConsumer(ctx context.Context, upsertConsumerRequest sdkkonnectgoops.UpsertConsumerRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpsertConsumerResponse, error)
	DeleteConsumer(ctx context.Context, controlPlaneID string, consumerID string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteConsumerResponse, error)
}
