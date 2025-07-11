package ops

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

// CreateKongDataPlaneClientCertificate creates a KongDataPlaneClientCertificate in Konnect.
// It sets the KonnectID in the KongDataPlaneClientCertificate status.
func CreateKongDataPlaneClientCertificate(
	ctx context.Context,
	sdk sdkops.DataPlaneClientCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	resp, err := sdk.CreateDataplaneCertificate(ctx,
		cpID,
		&sdkkonnectcomp.DataPlaneClientCertificateRequest{
			Cert: cert.Spec.Cert,
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}

	cert.SetKonnectID(*resp.DataPlaneClientCertificateResponse.Item.ID)

	return nil
}

// ListKongDataPlaneClientCertificates lists KongDataPlaneClientCertificates in Konnect.
// It returns an error if the operation fails.
func ListKongDataPlaneClientCertificates(
	ctx context.Context,
	sdk sdkops.DataPlaneClientCertificatesSDK,
	cpID string,
) ([]sdkkonnectcomp.DataPlaneClientCertificate, error) {
	resp, err := sdk.ListDpClientCertificates(ctx, cpID)
	if err != nil {
		return nil, err
	}

	if resp.ListDataPlaneCertificatesResponse == nil || resp.ListDataPlaneCertificatesResponse.Items == nil {
		return nil, nil
	}
	return resp.ListDataPlaneCertificatesResponse.Items, nil
}

// DeleteKongDataPlaneClientCertificate deletes a KongDataPlaneClientCertificate in Konnect.
// The KongDataPlaneClientCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func DeleteKongDataPlaneClientCertificate(
	ctx context.Context,
	sdk sdkops.DataPlaneClientCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteDataplaneCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}
