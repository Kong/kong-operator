package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// ensureEventGateway ensures the Konnect Event Gateway exists. For Origin entities it
// creates the gateway; for Mirror entities it looks up the existing gateway by ID.
func ensureEventGateway(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	eg *konnectv1alpha1.KonnectEventGateway,
) error {
	switch *eg.Spec.Source {
	case commonv1alpha1.EntitySourceOrigin:
		return createEventGateway(ctx, sdk, eg)
	case commonv1alpha1.EntitySourceMirror:
		id := string(eg.Spec.Mirror.Konnect.ID)
		resp, err := sdk.GetEventGateway(ctx, id)
		if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, eg); errWrap != nil {
			return errWrap
		}
		if resp == nil || resp.EventGatewayInfo == nil {
			return fmt.Errorf("failed getting %s: %w", eg.GetTypeName(), ErrNilResponse)
		}
		eg.SetKonnectID(resp.EventGatewayInfo.ID)
		return nil
	default:
		// CEL validation prevents reaching this branch.
		return fmt.Errorf("unsupported source type: %s", *eg.Spec.Source)
	}
}

// createEventGateway creates the Event Gateway in Konnect.
func createEventGateway(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	eg *konnectv1alpha1.KonnectEventGateway,
) error {
	req := sdkkonnectcomp.CreateGatewayRequest{
		Name:              eg.Spec.CreateGatewayRequest.Name,
		Description:       eg.Spec.CreateGatewayRequest.Description,
		MinRuntimeVersion: eg.Spec.CreateGatewayRequest.MinRuntimeVersion,
		Labels:            WithKubernetesMetadataLabels(eg, eg.Spec.CreateGatewayRequest.Labels),
	}

	resp, err := sdk.CreateEventGateway(ctx, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, eg); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.EventGatewayInfo == nil || resp.EventGatewayInfo.ID == "" {
		return fmt.Errorf("failed creating %s: %w", eg.GetTypeName(), ErrNilResponse)
	}

	eg.SetKonnectID(resp.EventGatewayInfo.ID)
	return nil
}

// updateEventGateway updates an existing Origin Event Gateway in Konnect.
func updateEventGateway(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	eg *konnectv1alpha1.KonnectEventGateway,
) error {
	id := eg.GetKonnectStatus().GetKonnectID()
	req := sdkkonnectcomp.UpdateGatewayRequest{
		Name:              &eg.Spec.CreateGatewayRequest.Name,
		Description:       eg.Spec.CreateGatewayRequest.Description,
		MinRuntimeVersion: eg.Spec.CreateGatewayRequest.MinRuntimeVersion,
		Labels:            WithKubernetesMetadataLabels(eg, eg.Spec.CreateGatewayRequest.Labels),
	}

	resp, err := sdk.UpdateEventGateway(ctx, id, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, eg); errWrap != nil {
		return handleUpdateError(ctx, err, eg, func(ctx context.Context) error {
			return createEventGateway(ctx, sdk, eg)
		})
	}

	if resp == nil || resp.EventGatewayInfo == nil {
		return fmt.Errorf("failed updating %s: %w", eg.GetTypeName(), ErrNilResponse)
	}

	return nil
}

// deleteEventGateway deletes an Origin Event Gateway from Konnect.
// Mirror gateways are never deleted.
func deleteEventGateway(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	eg *konnectv1alpha1.KonnectEventGateway,
) error {
	if eg.Spec.Source != nil && *eg.Spec.Source == commonv1alpha1.EntitySourceMirror {
		return nil
	}

	id := eg.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteEventGateway(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, eg); errWrap != nil {
		return handleDeleteError(ctx, err, eg)
	}

	return nil
}

// getEventGatewayForUID lists Event Gateways filtered by spec name, then matches by
// Kubernetes UID label. Returns ("", nil) when not found (triggers a create).
func getEventGatewayForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	eg *konnectv1alpha1.KonnectEventGateway,
) (string, error) {
	listResp, err := sdk.ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
		Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
			Name: &sdkkonnectcomp.StringFieldContainsFilter{
				Contains: eg.Spec.CreateGatewayRequest.Name,
			},
		},
	})
	if errWrap := wrapErrIfKonnectOpFailed(err, GetOp, eg); errWrap != nil {
		return "", errWrap
	}

	if listResp == nil || listResp.ListEventGatewaysResponse == nil {
		return "", nil
	}

	uid := string(eg.GetUID())
	for _, gw := range listResp.ListEventGatewaysResponse.Data {
		if v, ok := gw.Labels[KubernetesUIDLabelKey]; ok && v == uid {
			return gw.ID, nil
		}
	}

	return "", nil
}
