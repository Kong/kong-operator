package ops

// TODO: This file is hand-written and will be replaced with generated code.
// https://github.com/Kong/kong-operator/issues/3857

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func createKonnectEventControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	cp *konnectv1alpha1.KonnectEventControlPlane,
) error {
	req, err := cp.Spec.APISpec.ToCreateGatewayRequest()
	if err != nil {
		return fmt.Errorf("failed creating %s SDK request: %w", cp.GetTypeName(), err)
	}
	req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)

	resp, err := sdk.CreateEventGateway(ctx, *req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.EventGatewayInfo == nil || resp.EventGatewayInfo.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	cp.SetKonnectID(resp.EventGatewayInfo.ID)
	return nil
}

func updateKonnectEventControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	cp *konnectv1alpha1.KonnectEventControlPlane,
) error {
	req, err := cp.Spec.APISpec.ToUpdateGatewayRequest()
	if err != nil {
		return fmt.Errorf("failed updating %s SDK request: %w", cp.GetTypeName(), err)
	}
	req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)

	resp, err := sdk.UpdateEventGateway(ctx, cp.GetKonnectID(), *req)
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cp); errWrap != nil {
		return handleUpdateError(ctx, err, cp, func(ctx context.Context) error {
			return createKonnectEventControlPlane(ctx, sdk, cp)
		})
	}

	if resp == nil || resp.EventGatewayInfo == nil || resp.EventGatewayInfo.ID == "" {
		return fmt.Errorf("failed updating %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	cp.SetKonnectID(resp.EventGatewayInfo.ID)
	return nil
}

func deleteKonnectEventControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	cp *konnectv1alpha1.KonnectEventControlPlane,
) error {
	_, err := sdk.DeleteEventGateway(ctx, cp.GetKonnectID())
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		return handleDeleteError(ctx, err, cp)
	}

	return nil
}

func getKonnectEventControlPlaneForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewaysSDK,
	cp *konnectv1alpha1.KonnectEventControlPlane,
) (string, error) {
	resp, err := sdk.ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
		Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
			Name: &sdkkonnectcomp.StringFieldContainsFilter{
				Contains: string(cp.Spec.APISpec.Name),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), err)
	}
	if resp == nil || resp.ListEventGatewaysResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.ListEventGatewaysResponse.Data {
		if entry.GetLabels()[KubernetesUIDLabelKey] != string(cp.GetUID()) {
			continue
		}
		if entry.GetID() != "" {
			return entry.GetID(), nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: cp}
}
