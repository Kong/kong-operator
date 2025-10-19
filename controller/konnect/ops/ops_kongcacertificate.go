package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
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

func adoptCACertificate(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}

	adoptOptions := cert.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetCaCertificate(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.CACertificate == nil {
		return fmt.Errorf("failed to adopt %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.CACertificate.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(cert.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}

	switch adoptMode {
	case commonv1alpha1.AdoptModeOverride:
		certCopy := cert.DeepCopy()
		certCopy.SetKonnectID(konnectID)
		if err = updateCACertificate(ctx, sdk, certCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !caCertificateMatch(resp.CACertificate, cert) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	cert.SetKonnectID(konnectID)
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

func caCertificateMatch(
	konnectCert *sdkkonnectcomp.CACertificate,
	cert *configurationv1alpha1.KongCACertificate,
) bool {
	if konnectCert == nil {
		return false
	}

	return konnectCert.Cert == cert.Spec.Cert
}
