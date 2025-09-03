package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// createCACertificate creates a KongCACertificate in Konnect.
// It sets the KonnectID the KongCACertificate status.
func createCACertificate(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
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
		return errWrap
	}

	if resp == nil || resp.CACertificate == nil || resp.CACertificate.ID == nil || *resp.CACertificate.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	// At this point, the CACertificate has been created successfully.
	cert.SetKonnectID(*resp.CACertificate.ID)

	return nil
}

// updateCACertificate updates a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the KongCACertificate does not have a KonnectID.
func updateCACertificate(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
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

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteCACertificate deletes a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCACertificate(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCaCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func kongCACertificateToCACertificateInput(cert *configurationv1alpha1.KongCACertificate) sdkkonnectcomp.CACertificate {
	return sdkkonnectcomp.CACertificate{
		Cert: cert.Spec.Cert,
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}
}

func getKongCACertificateForUID(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) (string, error) {
	resp, err := sdk.ListCaCertificate(ctx, sdkkonnectops.ListCaCertificateRequest{
		ControlPlaneID: cert.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(cert)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list %s: %w", cert.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cert)
}
