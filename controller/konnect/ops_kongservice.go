package konnect

import (
	"context"
	"errors"
	"fmt"

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

// ServicesSDK is the interface for the Konnect Service SDK.
type ServicesSDK interface {
	CreateService(ctx context.Context, controlPlaneID string, service sdkkonnectgocomp.ServiceInput, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.CreateServiceResponse, error)
	UpsertService(ctx context.Context, req sdkkonnectgoops.UpsertServiceRequest, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.UpsertServiceResponse, error)
	DeleteService(ctx context.Context, controlPlaneID, serviceID string, opts ...sdkkonnectgoops.Option) (*sdkkonnectgoops.DeleteServiceResponse, error)
}

func createService(
	ctx context.Context,
	sdk ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", svc, client.ObjectKeyFromObject(svc))
	}

	resp, err := sdk.CreateService(ctx,
		svc.Status.Konnect.ControlPlaneID,
		kongServiceToSDKServiceInput(svc),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, svc); errWrapped != nil {
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
			KonnectEntityProgrammedReasonProgrammed,
			"",
			svc.GetGeneration(),
		),
		svc,
	)

	return nil
}

// updateService updates the Konnect Service entity.
// It is assumed that provided KongService has Konnect ID set in status.
// It returns an error if the KongService does not have a ControlPlaneRef or
// if the operation fails.
func updateService(
	ctx context.Context,
	sdk ServicesSDK,
	cl client.Client,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.Spec.ControlPlaneRef == nil {
		return fmt.Errorf("can't update %T without a ControlPlaneRef", svc)
	}

	// TODO(pmalek) handle other types of CP ref
	// TODO(pmalek) handle cross namespace refs
	nnCP := types.NamespacedName{
		Namespace: svc.Namespace,
		Name:      svc.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	var cp konnectv1alpha1.KonnectControlPlane
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectControlPlane %s: for %T %s: %w",
			nnCP, svc, client.ObjectKeyFromObject(svc), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectControlPlane %s does not have the Konnect ID",
			svc, nnCP,
		)
	}

	resp, err := sdk.UpsertService(ctx,
		sdkkonnectgoops.UpsertServiceRequest{
			ControlPlaneID: cp.Status.ID,
			ServiceID:      svc.GetKonnectStatus().GetKonnectID(),
			Service:        kongServiceToSDKServiceInput(svc),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, svc); errWrapped != nil {
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
			KonnectEntityProgrammedReasonProgrammed,
			"",
			svc.GetGeneration(),
		),
		svc,
	)

	return nil
}

// deleteService deletes a KongService in Konnect.
// It is assumed that provided KongService has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteService(
	ctx context.Context,
	sdk ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	id := svc.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteService(ctx, svc.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, svc); errWrapped != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkerrors.SDKError
		if errors.As(errWrapped, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", svc.GetTypeName(), "id", id,
					)
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongService]{
					Op:  DeleteOp,
					Err: sdkError,
				}
			}
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
