package ops

import (
	"context"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

// CreateAPIGatewayDataPlaneClientCertificate creates an API Gateway DataPlaneClientCertificate in Konnect.
// It sets the KonnectID in the KongDataPlaneClientCertificate status.
func CreateAPIGatewayDataPlaneClientCertificate(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewayDataPlaneCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	resp, err := sdk.CreateAPIGatewayDataPlaneCertificate(ctx,
		cpID,
		&sdkkonnectcomp.CreateAPIGatewayDataPlaneCertificateRequest{
			Certificate: cert.Spec.Cert,
			// TODO(pmalek): Add name and description fields to the CRD.
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}

	cert.SetKonnectID(resp.APIGatewayDataPlaneCertificate.ID)

	return nil
}

// ListAPIGatewayDataPlaneClientCertificates lists API Gateway DataPlaneClientCertificates in Konnect.
// It returns an error if the operation fails.
func ListAPIGatewayDataPlaneClientCertificates(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewayDataPlaneCertificatesSDK,
	cpID string,
) ([]sdkkonnectcomp.APIGatewayDataPlaneCertificate, error) {
	resp, err := sdk.ListAPIGatewayDataPlaneCertificates(ctx, sdkkonnectops.ListAPIGatewayDataPlaneCertificatesRequest{
		GatewayID: cpID,
	})
	if err != nil {
		return nil, err
	}

	if resp.ListAPIGatewayDataPlaneCertificatesResponse == nil || resp.ListAPIGatewayDataPlaneCertificatesResponse.Data == nil {
		return nil, nil
	}
	return resp.ListAPIGatewayDataPlaneCertificatesResponse.Data, nil
}

// DeleteAPIGatewayDataPlaneClientCertificate deletes a APIGatewayDataPlaneClientCertificate in Konnect.
// The APIGatewayDataPlaneClientCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func DeleteAPIGatewayDataPlaneClientCertificate(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewayDataPlaneCertificatesSDK,
	cert *configurationv1alpha1.KongDataPlaneClientCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteAPIGatewayDataPlaneCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}
