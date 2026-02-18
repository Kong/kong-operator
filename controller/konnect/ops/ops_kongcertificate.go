package ops

import (
	"context"
	"fmt"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

// createCertificate creates a KongCertificate in Konnect.
// It sets the KonnectID in the KongCertificate status.
func createCertificate(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	// NOTE: This is a workaround for the fact that the Konnect SDK does not
	// return a conflict error when creating a Certificate as there are no criteria
	// that would prevent the creation of a Certificate with the same spec fields.
	// This can be expanded to other entities by changing the order of get -> list
	// to list -> get in ops.go's Create() function.

	// Check if the Certificate already exists in Konnect: it is possible that
	// the Certificate was already created and cache the controller is using
	// is outdated, so we can't rely on the status.konnect.id.
	// If it does, set the KonnectID in the KongCertificate status and return.

	respList, err := sdk.ListCertificate(ctx, sdkkonnectops.ListCertificateRequest{
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(strings.Join(GenerateTagsForObject(cert, cert.Spec.Tags...), ",")),
	})
	if err != nil {
		return fmt.Errorf("failed to list: %w", err)
	}
	if respList.Object != nil && len(respList.Object.Data) > 0 {
		certList := respList.Object.Data[0]
		if certList.ID == nil {
			return fmt.Errorf("failed listing: found a cert without ID")
		}
		cert.SetKonnectID(*certList.ID)
		return nil
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

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
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

func kongCertificateToCertificateInput(cert *configurationv1alpha1.KongCertificate) sdkkonnectcomp.Certificate {
	input := sdkkonnectcomp.Certificate{
		Cert: cert.Spec.Cert,
		Key:  cert.Spec.Key,
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}
	if cert.Spec.CertAlt != "" {
		input.CertAlt = lo.ToPtr(cert.Spec.CertAlt)
	}
	if cert.Spec.KeyAlt != "" {
		input.KeyAlt = lo.ToPtr(cert.Spec.KeyAlt)
	}

	return input
}

func getKongCertificateForUID(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
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

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cert)
}
