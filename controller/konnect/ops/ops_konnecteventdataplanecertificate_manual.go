package ops

// TODO: This file is hand-written and will be replaced with generated code.
// https://github.com/kong/kong-operator/issues/3857

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func getKonnectEventDataPlaneCertificateForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (string, error) {
	gatewayID := certGatewayID(cert)
	if gatewayID == "" {
		return "", CantPerformOperationWithoutEventGatewayIDError{Entity: cert, Op: GetOp}
	}

	resp, err := sdk.ListEventGatewayDataPlaneCertificates(ctx, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), err)
	}
	if resp == nil || resp.ListEventGatewayDataPlaneCertificatesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.ListEventGatewayDataPlaneCertificatesResponse.Data {
		if !eventGatewayDataPlaneCertificateMatchesSpec(entry, cert) {
			continue
		}
		if entry.ID != "" {
			return entry.ID, nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: cert}
}

func certGatewayID(cert *konnectv1alpha1.KonnectEventDataPlaneCertificate) string {
	if cert.Status.GatewayID == nil {
		return ""
	}
	return cert.Status.GatewayID.ID
}

func eventGatewayDataPlaneCertificateMatchesSpec(
	remote sdkkonnectcomp.EventGatewayDataPlaneCertificate,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) bool {
	if remote.Certificate != cert.Spec.APISpec.Certificate {
		return false
	}

	remoteName := ""
	if remote.Name != nil {
		remoteName = *remote.Name
	}
	if remoteName != cert.Spec.APISpec.Name {
		return false
	}

	remoteDescription := ""
	if remote.Description != nil {
		remoteDescription = *remote.Description
	}
	return remoteDescription == cert.Spec.APISpec.Description
}
