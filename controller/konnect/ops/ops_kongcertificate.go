package ops

import (
	"context"
	"errors"
	"fmt"
	"github.com/samber/lo"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createCertificate creates a KongCertificate in Konnect.
// It sets the KonnectID and the Programmed condition in the KongCertificate status.
func createCertificate(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	resp, err := sdk.CreateCertificate(ctx,
		cpID,
		kongCertificateToCertificateInput(cert),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Certificate == nil || resp.Certificate.ID == nil || *resp.Certificate.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	// At this point, the Certificate has been created successfully.
	cert.SetKonnectID(*resp.Certificate.ID)

	return nil
}

// updateCertificate updates a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the KongCertificate does not have a KonnectID.
func updateCertificate(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: UpdateOp}
	}

	_, err := sdk.UpsertCertificate(ctx,
		sdkkonnectops.UpsertCertificateRequest{
			ControlPlaneID: cpID,
			CertificateID:  cert.GetKonnectStatus().GetKonnectID(),
			Certificate:    kongCertificateToCertificateInput(cert),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		// Certificate update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				if err := createCertificate(ctx, sdk, cert); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongCertificate]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				// Create succeeded, createCertificate sets the status so no need to do this here.

				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongCertificate]{
					Op:  UpdateOp,
					Err: sdkError,
				}
			}
		}
		return errWrap
	}

	return nil
}

// deleteCertificate deletes a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCertificate(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func kongCertificateToCertificateInput(cert *configurationv1alpha1.KongCertificate) sdkkonnectcomp.CertificateInput {
	return sdkkonnectcomp.CertificateInput{
		Cert: cert.Spec.Cert,
		Key:  cert.Spec.Key,
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}
}

func getKongCertificateForUID(
	ctx context.Context,
	sdk CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) (string, error) {
	resp, err := sdk.ListCertificate(ctx, sdkkonnectops.ListCertificateRequest{
		ControlPlaneID: cert.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(cert)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list %s: %w", cert.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.Object.Data), cert)
}
