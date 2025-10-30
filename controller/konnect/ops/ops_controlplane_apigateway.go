package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// // ensureControlPlane ensures that the Konnect ControlPlane exists in Konnect. It is created
// // if it does not exist and the source is Origin. If the source is Mirror, it checks
// // if the ControlPlane exists in Konnect and returns an error if it does not.
// func ensureControlPlane(
// 	ctx context.Context,
// 	sdk sdkops.ControlPlaneSDK,
// 	sdkGroups sdkops.ControlPlaneGroupSDK,
// 	cl client.Client,
// 	cp *konnectv1alpha2.KonnectGatewayControlPlane,
// ) error {
// 	switch *cp.Spec.Source {
// 	case commonv1alpha1.EntitySourceOrigin:
// 		return createAPIGateway(ctx, sdk, sdkGroups, cl, cp)
// 	case commonv1alpha1.EntitySourceMirror:
// 		resp, err := GetControlPlaneByID(
// 			ctx,
// 			sdk,
// 			// not nilness is ensured by CEL rules
// 			string(cp.Spec.Mirror.Konnect.ID),
// 		)
// 		if err != nil {
// 			return err
// 		}
// 		cp.SetKonnectID(string(cp.Spec.Mirror.Konnect.ID))
// 		cp.Status.ClusterType = resp.Config.ClusterType
// 		return nil
// 	default:
// 		// This should never happen, as the source type is validated by CEL rules.
// 		return fmt.Errorf("unsupported source type: %s", *cp.Spec.Source)
// 	}
// }

// createAPIGateway creates the API Gateway.
func createAPIGateway(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewaysSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	// req := cp.Spec.CreateControlPlaneRequest
	// req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)

	req := sdkkonnectcomp.CreateGatewayRequest{
		Name:        cp.Spec.CreateControlPlaneRequest.Name,
		Description: cp.Spec.CreateControlPlaneRequest.Description,
		Labels:      WithKubernetesMetadataLabels(cp, cp.Spec.CreateControlPlaneRequest.Labels),
	}

	resp, err := sdk.CreateAPIGateway(ctx, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Gateway == nil || resp.Gateway.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	// At this point, the Gateway has been created in Konnect.
	id := resp.Gateway.ID
	cp.SetKonnectID(id)

	return nil
}

// deleteAPIGateway deletes a Konnect API Gateway.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
func deleteAPIGateway(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewaysSDK,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteAPIGateway(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		return handleDeleteError(ctx, err, cp)
	}

	return nil
}

// updateAPIGateway updates a Konnect API Gateway.
func updateAPIGateway(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewaysSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	// if the source type is Mirror, don't touch the Konnect entity.
	if isMirrorEntity(cp) {
		return nil
	}
	id := cp.GetKonnectStatus().GetKonnectID()
	req := sdkkonnectcomp.UpdateGatewayRequest{
		Name:        sdkkonnectgo.String(cp.GetKonnectName()),
		Description: cp.GetKonnectDescription(),
		Labels:      WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
	}

	resp, err := sdk.UpdateAPIGateway(ctx, id, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cp); errWrap != nil {
		return handleUpdateError(ctx, err, cp, func(ctx context.Context) error {
			return createAPIGateway(ctx, sdk, cl, cp)
		})
	}

	if resp == nil || resp.Gateway == nil {
		return fmt.Errorf("failed updating API Gateway: %w", ErrNilResponse)
	}
	id = resp.Gateway.ID

	return nil
}

// GetAPIGatewayByID returns the Konnect API Gateway that matches the provided ID.
func GetAPIGatewayByID(
	ctx context.Context,
	sdk sdkkonnectgo.APIGatewaysSDK,
	id string,
) (*sdkkonnectcomp.Gateway, error) {
	resp, err := sdk.GetAPIGateway(ctx, id)
	if err != nil || resp == nil || resp.Gateway == nil {
		return nil, fmt.Errorf("failed getting API Gateway with id %s: %w", id, err)
	}

	return resp.Gateway, nil
}
