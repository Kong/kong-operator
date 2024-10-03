package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createKongDataPlaneClientCertificate creates a KongDataPlaneClientCertificate in Konnect.
// It sets the KonnectID and the Programmed condition in the KongDataPlaneClientCertificate status.
func createKongDataPlaneClientCertificate(
	ctx context.Context,
	sdk DataPlaneClientCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", cert, client.ObjectKeyFromObject(cert))
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
		SetKonnectEntityProgrammedConditionFalse(cert, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cert.Status.Konnect.SetKonnectID(*resp.DataPlaneClientCertificate.Item.ID)
	SetKonnectEntityProgrammedCondition(cert)

	return nil
}

// deleteKongDataPlaneClientCertificate deletes a KongDataPlaneClientCertificate in Konnect.
// The KongDataPlaneClientCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteKongDataPlaneClientCertificate(
	ctx context.Context,
	sdk DataPlaneClientCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteDataplaneCertificate(ctx, cert.GetControlPlaneID(), id)
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
			return FailedKonnectOpError[configurationv1alpha1.KongDataPlaneClientCertificate]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongDataPlaneClientCertificate]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}
