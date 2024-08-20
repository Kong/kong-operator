package ops

import (
	"context"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ServicesSDK is the interface for the Konnect Service SDK.
type ServicesSDK interface {
	CreateService(ctx context.Context, controlPlaneID string, service sdkkonnectgocomp.ServiceInput, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateServiceResponse, error)
	UpsertService(ctx context.Context, req sdkkonnectgoops.UpsertServiceRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpsertServiceResponse, error)
	DeleteService(ctx context.Context, controlPlaneID, serviceID string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteServiceResponse, error)
}
