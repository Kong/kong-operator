package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
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

// adoptKongDataPlaneCertificate adopts an existing datplane certificates in Konnect.
func adoptKongDataPlaneCertificate(
	ctx context.Context,
	sdk sdkops.DataPlaneClientCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	adoptOptions := cert.GetAdoptOptions()
	if adoptOptions.Konnect == nil || adoptOptions.Konnect.ID == "" {
		return fmt.Errorf("konnect ID must be provided for adoption")
	}
	if adoptOptions.Mode != "" && adoptOptions.Mode != commonv1alpha1.AdoptModeMatch {
		return fmt.Errorf("only match mode adoption is supported for DataPlane certificate, got mode: %q", adoptOptions.Mode)
	}

	konnectID := adoptOptions.Konnect.ID
	resp, err := sdk.GetDataplaneCertificate(ctx, cpID, konnectID)

	if err != nil {
		return KonnectEntityAdoptionFetchError{KonnectID: konnectID, Err: err}
	}
	if resp == nil || resp.DataPlaneClientCertificateResponse == nil {
		return fmt.Errorf("failed getting %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	// check if the cert in the CR matches the adopted DP cert.
	if resp.DataPlaneClientCertificateResponse.Item == nil ||
		resp.DataPlaneClientCertificateResponse.Item.Cert == nil ||
		cert.Spec.Cert != *resp.DataPlaneClientCertificateResponse.Item.Cert {
		return KonnectEntityAdoptionNotMatchError{
			KonnectID: konnectID,
		}
	}

	cert.SetKonnectID(konnectID)
	return nil
}
