package ops

import (
	"context"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// getEventGatewayDataPlaneCertificateForUID finds the Konnect data plane
// certificate of obj. Konnect has no labels on Event Gateway data plane
// certificates, so there is no k8s-uid label to match: this matches the
// certificate whose certificate, name and description equal obj's, with obj's
// Secret-sourced certificate resolved. A certificate with the same name but
// another certificate (e.g. created outside the operator) is never matched; an
// identical one cannot be told apart from obj's own.
func getEventGatewayDataPlaneCertificateForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cl client.Client,
	obj *configurationv1alpha1.EventGatewayDataPlaneCertificate,
) (string, error) {
	gatewayID := obj.GetGatewayID()
	if gatewayID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "KonnectEventGateway", Op: GetOp}
	}

	want, err := obj.ToCreateEventGatewayDataPlaneCertificateRequest(ctx, cl)
	if err != nil {
		// Without obj's certificate (e.g. its Secret is gone) no certificate can
		// be matched safely. Report it as not found rather than as an error: on
		// deletion, an error would block removing the object's finalizer, e.g.
		// for an object never created in Konnect because its Secret is missing.
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	resp, err := sdk.ListEventGatewayDataPlaneCertificates(ctx, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil || resp.ListEventGatewayDataPlaneCertificatesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.ListEventGatewayDataPlaneCertificatesResponse.Data {
		if strings.TrimSpace(entry.GetCertificate()) != strings.TrimSpace(want.GetCertificate()) ||
			stringValueGeneric(entry.GetName()) != stringValueGeneric(want.GetName()) ||
			stringValueGeneric(entry.GetDescription()) != stringValueGeneric(want.GetDescription()) {
			continue
		}
		if entry.GetID() != "" {
			return entry.GetID(), nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}
