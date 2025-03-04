package ops

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createKongDataPlaneClientCertificate creates a KongDataPlaneClientCertificate in Konnect.
// It sets the KonnectID in the KongDataPlaneClientCertificate status.
func createKongDataPlaneClientCertificate(
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

// deleteKongDataPlaneClientCertificate deletes a KongDataPlaneClientCertificate in Konnect.
// The KongDataPlaneClientCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteKongDataPlaneClientCertificate(
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
