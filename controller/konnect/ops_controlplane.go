package konnect

import (
	"context"
	"errors"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ControlPlaneSDK is the interface for the Konnect ControlPlane SDK.
type ControlPlaneSDK interface {
	CreateControlPlane(ctx context.Context, req components.CreateControlPlaneRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateControlPlaneResponse, error)
	DeleteControlPlane(ctx context.Context, id string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteControlPlaneResponse, error)
	UpdateControlPlane(ctx context.Context, id string, req components.UpdateControlPlaneRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpdateControlPlaneResponse, error)
}

func createControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	resp, err := sdk.CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest)
	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	// TODO: implement entity adoption https://github.com/Kong/gateway-operator/issues/460
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
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
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReasonProgrammed,
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
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteControlPlane(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		var sdkNotFoundError *sdkerrors.NotFoundError
		if errors.As(err, &sdkNotFoundError) {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", cp.GetTypeName(), "id", id,
				)
			return nil
		}
		var sdkError *sdkerrors.SDKError
		if errors.As(errWrap, &sdkError) {
			return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
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
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	req := components.UpdateControlPlaneRequest{
		Name:        sdkkonnectgo.String(cp.Spec.Name),
		Description: cp.Spec.Description,
		AuthType:    (*components.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
		ProxyUrls:   cp.Spec.ProxyUrls,
		Labels:      cp.Spec.Labels,
	}

	resp, err := sdk.UpdateControlPlane(ctx, id, req)
	var sdkError *sdkerrors.NotFoundError
	if errors.As(err, &sdkError) {
		ctrllog.FromContext(ctx).
			Info("entity not found in Konnect, trying to recreate",
				"type", cp.GetTypeName(), "id", id,
			)
		if err := createControlPlane(ctx, sdk, cp); err != nil {
			return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
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
				KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToUpdate",
				errWrap.Error(),
				cp.GetGeneration(),
			),
			cp,
		)
		return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
			Op:  UpdateOp,
			Err: errWrap,
		}
	}

	cp.Status.SetKonnectID(resp.ControlPlane.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReasonProgrammed,
			"",
			cp.GetGeneration(),
		),
		cp,
	)

	return nil
}
