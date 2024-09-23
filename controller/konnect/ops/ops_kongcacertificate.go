package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
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
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", cert, client.ObjectKeyFromObject(cert))
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
		return fmt.Errorf("can't update %T without a ControlPlaneID", cert)
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
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", cert.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongCACertificate]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongCACertificate]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func kongCACertificateToCACertificateInput(cert *configurationv1alpha1.KongCACertificate) sdkkonnectcomp.CACertificateInput {
	var (
		annotationTags = metadata.ExtractTags(cert)
		specTags       = cert.Spec.Tags
		k8sMetaTags    = GenerateKubernetesMetadataTags(cert)
	)
	return sdkkonnectcomp.CACertificateInput{
		Cert: cert.Spec.Cert,
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: lo.Uniq(slices.Concat(annotationTags, specTags, k8sMetaTags)),
	}
}
