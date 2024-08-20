package ops

import (
	"context"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ConsumerGroupSDK is the interface for the Konnect ConsumerGroups SDK.
type ConsumerGroupSDK interface {
	CreateConsumerGroup(ctx context.Context, controlPlaneID string, consumerInput sdkkonnectgocomp.ConsumerGroupInput, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateConsumerGroupResponse, error)
	UpsertConsumerGroup(ctx context.Context, upsertConsumerRequest sdkkonnectgoops.UpsertConsumerGroupRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpsertConsumerGroupResponse, error)
	DeleteConsumerGroup(ctx context.Context, controlPlaneID string, consumerID string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteConsumerGroupResponse, error)
}
