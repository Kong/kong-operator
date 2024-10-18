package ops

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createCACertificate creates a KongCACertificate in Konnect.
// It sets the KonnectID and the Programmed condition in the KongCACertificate status.
func createCACertificate(
	ctx context.Context,
	sdk CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	resp, err := sdk.CreateCaCertificate(ctx,
		cpID,
		kongCACertificateToCACertificateInput(cert),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cert, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cert.Status.Konnect.SetKonnectID(*resp.CACertificate.ID)
	SetKonnectEntityProgrammedCondition(cert)

	return nil
}

// updateCACertificate updates a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the KongCACertificate does not have a KonnectID.
func updateCACertificate(
	ctx context.Context,
	sdk CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: UpdateOp}
	}

	_, err := sdk.UpsertCaCertificate(ctx,
		sdkkonnectops.UpsertCaCertificateRequest{
			ControlPlaneID:  cpID,
			CACertificateID: cert.GetKonnectStatus().GetKonnectID(),
			CACertificate:   kongCACertificateToCACertificateInput(cert),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cert, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(cert)

	return nil
}

// deleteCACertificate deletes a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCACertificate(
	ctx context.Context,
	sdk CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCaCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func kongCACertificateToCACertificateInput(cert *configurationv1alpha1.KongCACertificate) sdkkonnectcomp.CACertificateInput {
	return sdkkonnectcomp.CACertificateInput{
		Cert: cert.Spec.Cert,
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}
}
