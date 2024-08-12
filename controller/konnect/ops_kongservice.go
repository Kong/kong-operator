package konnect

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createService(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", svc, client.ObjectKeyFromObject(svc))
	}

	resp, err := sdk.Services.CreateService(ctx,
		svc.Status.Konnect.ControlPlaneID,
		kongServiceToSDKServiceInput(svc),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[konnectv1alpha1.KonnectControlPlane](err, CreateOp); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				svc.GetGeneration(),
			),
			svc,
		)
		return errWrapped
	}

	svc.Status.Konnect.SetKonnectID(*resp.Service.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReason,
			"",
			svc.GetGeneration(),
		),
		svc,
	)

	return nil
}

func updateService(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	cl client.Client,
	svc *configurationv1alpha1.KongService,
) error {
	// TODO(pmalek) handle other types of CP ref
	nnCP := types.NamespacedName{
		Namespace: svc.Spec.ControlPlaneRef.KonnectNamespacedRef.Namespace,
		Name:      svc.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	if nnCP.Namespace == "" {
		nnCP.Namespace = svc.Namespace
	}
	var cp konnectv1alpha1.KonnectControlPlane
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectControlPlane %s: for KongService %s: %w",
			nnCP, client.ObjectKeyFromObject(svc), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectControlPlane %s does not have the Konnect ID",
			svc, nnCP,
		)
	}
	if svc.Status.Konnect == nil || svc.Status.Konnect.ID == "" {
		return fmt.Errorf(
			"can't update %T %s when it does not have the Konnect ID",
			svc, client.ObjectKeyFromObject(svc),
		)
	}

	resp, err := sdk.Services.UpsertService(ctx,
		sdkkonnectgoops.UpsertServiceRequest{
			ControlPlaneID: cp.Status.ID,
			ServiceID:      svc.GetKonnectStatus().GetKonnectID(),
			Service:        kongServiceToSDKServiceInput(svc),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongService](err, UpdateOp); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				svc.GetGeneration(),
			),
			svc,
		)
		return errWrapped
	}

	svc.Status.Konnect.SetKonnectID(*resp.Service.ID)
	svc.Status.Konnect.SetControlPlaneID(cp.Status.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReason,
			"",
			svc.GetGeneration(),
		),
		svc,
	)

	return nil
}

func deleteService(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	svc *configurationv1alpha1.KongService,
) error {
	id := svc.Status.Konnect.GetKonnectID()
	if id == "" {
		return fmt.Errorf("can't remove %T without a Konnect ID", svc)
	}

	_, err := sdk.Services.DeleteService(ctx, svc.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongService](err, DeleteOp); errWrapped != nil {
		var sdkError *sdkerrors.NotFoundError
		if errors.As(errWrapped, &sdkError) {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", svc.GetTypeName(), "id", id,
				)
			return nil
		}
		return FailedKonnectOpError[configurationv1alpha1.KongService]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongServiceToSDKServiceInput(
	svc *configurationv1alpha1.KongService,
) sdkkonnectgocomp.ServiceInput {
	return sdkkonnectgocomp.ServiceInput{
		URL:            svc.Spec.KongServiceAPISpec.URL,
		ConnectTimeout: svc.Spec.KongServiceAPISpec.ConnectTimeout,
		Enabled:        svc.Spec.KongServiceAPISpec.Enabled,
		Host:           svc.Spec.KongServiceAPISpec.Host,
		Name:           svc.Spec.KongServiceAPISpec.Name,
		Path:           svc.Spec.KongServiceAPISpec.Path,
		Port:           svc.Spec.KongServiceAPISpec.Port,
		Protocol:       svc.Spec.KongServiceAPISpec.Protocol,
		ReadTimeout:    svc.Spec.KongServiceAPISpec.ReadTimeout,
		Retries:        svc.Spec.KongServiceAPISpec.Retries,
		Tags:           svc.Spec.KongServiceAPISpec.Tags,
		TLSVerify:      svc.Spec.KongServiceAPISpec.TLSVerify,
		TLSVerifyDepth: svc.Spec.KongServiceAPISpec.TLSVerifyDepth,
		WriteTimeout:   svc.Spec.KongServiceAPISpec.WriteTimeout,
	}
}
