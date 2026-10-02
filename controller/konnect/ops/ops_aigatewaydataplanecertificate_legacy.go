package ops

import (
	"context"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// getLegacyAIGatewayDataPlaneCertificateForUID finds the Konnect data plane
// certificate of obj when it carries no k8s-uid label: certificates created
// before the operator labeled them never get one, as Konnect cannot update
// them. It matches, among unlabeled certificates only, the one whose
// certificate (resolved from its Secret), title and description equal obj's,
// so a certificate owned by another object or with another certificate is
// never matched.
func getLegacyAIGatewayDataPlaneCertificateForUID(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayDataPlaneCertificatesSDK,
	cl client.Client,
	obj *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
) (string, error) {
	parentID := obj.GetGatewayID()
	if parentID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "KonnectAIGateway", Op: GetOp}
	}

	want, err := obj.ToCreateAIGatewayDataPlaneCertificateRequest(ctx, cl)
	if err != nil {
		// Without obj's certificate (e.g. its Secret is gone) no certificate can
		// be matched safely. Report it as not found rather than as an error: on
		// deletion, an error would block removing the object's finalizer, e.g.
		// for an object never created in Konnect because its Secret is missing.
		return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
	}

	var pageAfter *string
	// Cursors already requested, to detect a next-page cursor that does not
	// advance (directly or through a longer cycle).
	seenCursors := map[string]struct{}{}
	for {
		resp, err := sdk.ListAiGatewayDataPlaneCertificates(ctx, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{
			GatewayID: parentID,
			PageSize:  new(listPageSize),
			PageAfter: pageAfter,
		})
		if err != nil {
			return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
		}
		if resp == nil || resp.ListAIGatewayDataPlaneCertificatesResponse == nil {
			return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
		}

		for _, entry := range resp.ListAIGatewayDataPlaneCertificatesResponse.Data {
			if _, labeled := entry.GetLabels()[KubernetesUIDLabelKey]; labeled {
				continue
			}
			if strings.TrimSpace(entry.GetCert()) != strings.TrimSpace(want.GetCert()) ||
				entry.GetTitle() != want.GetTitle() ||
				stringValueGeneric(entry.GetDescription()) != stringValueGeneric(want.GetDescription()) {
				continue
			}
			if entry.GetID() != "" {
				return entry.GetID(), nil
			}
		}

		meta := resp.ListAIGatewayDataPlaneCertificatesResponse.GetMeta()
		page := meta.GetPage()
		if pageAfter, err = nextPageCursor(page.GetNext(), seenCursors); err != nil {
			return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
		}
		if pageAfter == nil {
			break
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}
