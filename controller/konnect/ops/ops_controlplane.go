package ops

import (
	"context"
	"errors"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	resp, err := sdk.CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest)
	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	// TODO: implement entity adoption https://github.com/Kong/gateway-operator/issues/460
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrap.Error(),
				cp.GetGeneration(),
			),
			cp,
		)
		return errWrap
	}

	cp.Status.SetKonnectID(resp.ControlPlane.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			cp.GetGeneration(),
		),
		cp,
	)

	return nil
}

// deleteControlPlane deletes a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
func deleteControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteControlPlane(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		var sdkNotFoundError *sdkkonnecterrs.NotFoundError
		if errors.As(err, &sdkNotFoundError) {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", cp.GetTypeName(), "id", id,
				)
			return nil
		}
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

// updateControlPlane updates a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
// It returns an error if the operation fails.
func updateControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	req := sdkkonnectcomp.UpdateControlPlaneRequest{
		Name:        sdkkonnectgo.String(cp.Spec.Name),
		Description: cp.Spec.Description,
		AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
		ProxyUrls:   cp.Spec.ProxyUrls,
		Labels:      cp.Spec.Labels,
	}

	resp, err := sdk.UpdateControlPlane(ctx, id, req)
	var sdkError *sdkkonnecterrs.NotFoundError
	if errors.As(err, &sdkError) {
		ctrllog.FromContext(ctx).
			Info("entity not found in Konnect, trying to recreate",
				"type", cp.GetTypeName(), "id", id,
			)
		if err := createControlPlane(ctx, sdk, cp); err != nil {
			return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
				Op:  UpdateOp,
				Err: err,
			}
		}
		// Create succeeded, createControlPlane sets the status so no need to do this here.

		return nil
	}

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cp); errWrap != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToUpdate",
				errWrap.Error(),
				cp.GetGeneration(),
			),
			cp,
		)
		return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
			Op:  UpdateOp,
			Err: errWrap,
		}
	}

	cp.Status.SetKonnectID(resp.ControlPlane.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			cp.GetGeneration(),
		),
		cp,
	)

	return nil
}
