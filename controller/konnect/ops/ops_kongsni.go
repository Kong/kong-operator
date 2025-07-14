package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createSNI(
	ctx context.Context,
	sdk sdkops.SNIsSDK,
	sni *configurationv1alpha1.KongSNI,
) error {
	cpID := sni.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: sni, Op: CreateOp}
	}
	if sni.Status.Konnect == nil || sni.Status.Konnect.CertificateID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect Certificate ID", sni, client.ObjectKeyFromObject(sni))
	}

	resp, err := sdk.CreateSniWithCertificate(ctx, sdkkonnectops.CreateSniWithCertificateRequest{
		ControlPlaneID:    cpID,
		CertificateID:     sni.Status.Konnect.CertificateID,
		SNIWithoutParents: kongSNIToSNIWithoutParents(sni),
	})
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, sni); errWrapped != nil {
		return errWrapped
	}

	if resp == nil || resp.Sni == nil || resp.Sni.ID == nil || *resp.Sni.ID == "" {
		return fmt.Errorf("failed creating %s: %w", sni.GetTypeName(), ErrNilResponse)
	}

	// At this point, the SNI has been created successfully.
	sni.Status.Konnect.SetKonnectID(*resp.Sni.ID)

	return nil
}

func updateSNI(
	ctx context.Context,
	sdk sdkops.SNIsSDK,
	sni *configurationv1alpha1.KongSNI,
) error {
	cpID := sni.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: sni, Op: UpdateOp}
	}
	if sni.Status.Konnect == nil || sni.Status.Konnect.CertificateID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect Certificate ID", sni, client.ObjectKeyFromObject(sni))
	}
	id := sni.GetKonnectID()

	_, err := sdk.UpsertSniWithCertificate(ctx, sdkkonnectops.UpsertSniWithCertificateRequest{
		ControlPlaneID:    cpID,
		CertificateID:     sni.Status.Konnect.CertificateID,
		SNIID:             id,
		SNIWithoutParents: kongSNIToSNIWithoutParents(sni),
	})

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, sni); errWrap != nil {
		return errWrap
	}

	return nil
}

func deleteSNI(
	ctx context.Context,
	sdk sdkops.SNIsSDK,
	sni *configurationv1alpha1.KongSNI,
) error {
	cpID := sni.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't delete %T %s without a Konnect ControlPlane ID", sni, client.ObjectKeyFromObject(sni))
	}
	if sni.Status.Konnect == nil || sni.Status.Konnect.CertificateID == "" {
		return fmt.Errorf("can't delete %T %s without a Konnect Certificate ID", sni, client.ObjectKeyFromObject(sni))
	}
	id := sni.GetKonnectID()

	_, err := sdk.DeleteSniWithCertificate(ctx, sdkkonnectops.DeleteSniWithCertificateRequest{
		ControlPlaneID: cpID,
		CertificateID:  sni.Status.Konnect.CertificateID,
		SNIID:          id,
	})

	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, sni); errWrapped != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			if sdkError.StatusCode == http.StatusNotFound {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", sni.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongSNI]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongSNI]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongSNIToSNIWithoutParents(sni *configurationv1alpha1.KongSNI) sdkkonnectcomp.SNIWithoutParents {
	return sdkkonnectcomp.SNIWithoutParents{
		Name: sni.Spec.Name,
		Tags: GenerateTagsForObject(sni, sni.Spec.Tags...),
	}
}

// getKongSNIForUID returns the Konnect ID of the Konnect SNI that matches the UID of the provided SNI.
func getKongSNIForUID(ctx context.Context, sdk sdkops.SNIsSDK, sni *configurationv1alpha1.KongSNI) (string, error) {
	resp, err := sdk.ListSni(ctx, sdkkonnectops.ListSniRequest{
		ControlPlaneID: sni.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(sni)),
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", sni.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", sni.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), sni)
}
