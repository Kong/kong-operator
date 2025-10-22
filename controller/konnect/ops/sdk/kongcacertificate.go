package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// CACertificatesSDK is the interface for the CACertificatesSDK.
type CACertificatesSDK interface {
	CreateCaCertificate(ctx context.Context, controlPlaneID string, caCertificate sdkkonnectcomp.CACertificate, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateCaCertificateResponse, error)
	GetCaCertificate(ctx context.Context, caCertificateID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetCaCertificateResponse, error)
	UpsertCaCertificate(ctx context.Context, request sdkkonnectops.UpsertCaCertificateRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertCaCertificateResponse, error)
	DeleteCaCertificate(ctx context.Context, controlPlaneID string, caCertificateID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteCaCertificateResponse, error)
	ListCaCertificate(ctx context.Context, request sdkkonnectops.ListCaCertificateRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListCaCertificateResponse, error)
}
