package konnect

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/Kong/sdk-konnect-go/models/components"
	"github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createControlPlane(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	logger logr.Logger, //nolint:unparam
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	resp, err := sdk.ControlPlanes.CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest)
	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	// TODO: implement entity adoption https://github.com/Kong/gateway-operator/issues/460
	if errWrap := wrapErrIfKonnectOpFailed[konnectv1alpha1.KonnectControlPlane](err, CreateOp); errWrap != nil {
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
			KonnectEntityProgrammedReason,
			"",
			cp.GetGeneration(),
		),
		cp,
	)

	return nil
}

func deleteControlPlane(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	logger logr.Logger,
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	if id == "" {
		return fmt.Errorf("can't remove %T without a Konnect ID", cp)
	}

	_, err := sdk.ControlPlanes.DeleteControlPlane(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed[konnectv1alpha1.KonnectControlPlane](err, DeleteOp); errWrap != nil {
		var sdkError *sdkerrors.NotFoundError
		if errors.As(err, &sdkError) {
			logger.Info("entity not found in Konnect, skipping delete",
				"op", DeleteOp, "type", cp.GetTypeName(), "id", id,
			)
			return nil
		}
		return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func updateControlPlane(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	logger logr.Logger,
	cp *konnectv1alpha1.KonnectControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	if id == "" {
		return fmt.Errorf("can't update %T without a Konnect ID", cp)
	}

	req := components.UpdateControlPlaneRequest{
		Name:        sdkkonnectgo.String(cp.Spec.Name),
		Description: cp.Spec.Description,
		AuthType:    (*components.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
		ProxyUrls:   cp.Spec.ProxyUrls,
		Labels:      cp.Spec.Labels,
	}

	resp, err := sdk.ControlPlanes.UpdateControlPlane(ctx, id, req)
	var sdkError *sdkerrors.NotFoundError
	if errors.As(err, &sdkError) {
		logger.Info("entity not found in Konnect, trying to recreate",
			"type", cp.GetTypeName(), "id", id,
		)
		if err := createControlPlane(ctx, sdk, logger, cp); err != nil {
			return FailedKonnectOpError[konnectv1alpha1.KonnectControlPlane]{
				Op:  UpdateOp,
				Err: err,
			}
		}
		// Create succeeded, createControlPlane sets the status so no need to do this here.

		return nil
	}

	if errWrap := wrapErrIfKonnectOpFailed[konnectv1alpha1.KonnectControlPlane](err, UpdateOp); errWrap != nil {
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
			KonnectEntityProgrammedReason,
			"",
			cp.GetGeneration(),
		),
		cp,
	)

	return nil
}
