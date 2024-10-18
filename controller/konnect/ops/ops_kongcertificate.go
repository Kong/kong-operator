package ops

import (
	"context"
	"errors"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createCertificate creates a KongCertificate in Konnect.
// It sets the KonnectID and the Programmed condition in the KongCertificate status.
func createCertificate(
	ctx context.Context,
	sdk CertificatesSDK,
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
		SetKonnectEntityProgrammedConditionFalse(cert, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cert.Status.Konnect.SetKonnectID(*resp.Certificate.ID)
	SetKonnectEntityProgrammedCondition(cert)

	return nil
}

// updateCertificate updates a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the KongCertificate does not have a KonnectID.
func updateCertificate(
	ctx context.Context,
	sdk CertificatesSDK,
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
		SetKonnectEntityProgrammedConditionFalse(cert, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(cert)

	return nil
}

// deleteCertificate deletes a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCertificate(
	ctx context.Context,
	sdk CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", cert.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongCertificate]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongCertificate]{
			Op:  DeleteOp,
			Err: errWrap,
		}
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
