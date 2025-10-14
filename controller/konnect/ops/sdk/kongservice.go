package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// ServicesSDK is the interface for the Konnect Service SDK.
type ServicesSDK interface {
	CreateService(ctx context.Context, controlPlaneID string, service sdkkonnectcomp.Service, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateServiceResponse, error)
	GetService(ctx context.Context, serviceID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetServiceResponse, error)
	UpsertService(ctx context.Context, req sdkkonnectops.UpsertServiceRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertServiceResponse, error)
	DeleteService(ctx context.Context, controlPlaneID, serviceID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteServiceResponse, error)
	ListService(ctx context.Context, request sdkkonnectops.ListServiceRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListServiceResponse, error)
}
