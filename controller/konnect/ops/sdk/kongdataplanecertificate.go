package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// DataPlaneClientCertificatesSDK is the interface for the DataPlaneClientCertificatesSDK.
type DataPlaneClientCertificatesSDK interface {
	CreateDataplaneCertificate(ctx context.Context, cpID string, dpReq *sdkkonnectcomp.DataPlaneClientCertificateRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateDataplaneCertificateResponse, error)
	DeleteDataplaneCertificate(ctx context.Context, controlPlaneID string, certificateID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteDataplaneCertificateResponse, error)
	ListDpClientCertificates(ctx context.Context, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListDpClientCertificatesResponse, error)
	GetDataplaneCertificate(ctx context.Context, controlPlaneID string, certificateID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetDataplaneCertificateResponse, error)
}
