package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// CertificatesSDK is the interface for the CertificatesSDK.
type CertificatesSDK interface {
	CreateCertificate(ctx context.Context, controlPlaneID string, certificate sdkkonnectcomp.Certificate, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateCertificateResponse, error)
	GetCertificate(ctx context.Context, certificateID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetCertificateResponse, error)
	UpsertCertificate(ctx context.Context, request sdkkonnectops.UpsertCertificateRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertCertificateResponse, error)
	DeleteCertificate(ctx context.Context, controlPlaneID string, certificateID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteCertificateResponse, error)
	ListCertificate(ctx context.Context, request sdkkonnectops.ListCertificateRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListCertificateResponse, error)
}
