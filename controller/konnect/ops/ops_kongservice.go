package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/pkg/consts"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createService(
	ctx context.Context,
	sdk ServicesSDK,
	svc *configurationv1alpha1.KongService,
) (string, consts.ConditionReason, error) { //nolint:unparam
	if svc.GetControlPlaneID() == "" {
		return "", "", fmt.Errorf(
			"can't create %T %s without a Konnect ControlPlane ID",
			svc, client.ObjectKeyFromObject(svc),
		)
	}

	resp, err := sdk.CreateService(ctx,
		svc.Status.Konnect.ControlPlaneID,
		kongServiceToSDKServiceInput(svc),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, svc); errWrap != nil {
		return "", "", errWrap
	}

	return *resp.Service.ID, "", nil
}

// updateService updates the Konnect Service entity.
// It is assumed that provided KongService has Konnect ID set in status.
// It returns an error if the KongService does not have a ControlPlaneRef or
// if the operation fails.
func updateService(
	ctx context.Context,
	sdk ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return fmt.Errorf("can't update  %T %s without a Konnect ControlPlane ID",
			svc, client.ObjectKeyFromObject(svc),
		)
	}

	id := svc.GetKonnectStatus().GetKonnectID()
	_, err := sdk.UpsertService(ctx,
		sdkkonnectops.UpsertServiceRequest{
			ControlPlaneID: svc.GetControlPlaneID(),
			ServiceID:      id,
			Service:        kongServiceToSDKServiceInput(svc),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, svc); errWrap != nil {
		// Service update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				id, _, err := createService(ctx, sdk, svc)
				if err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongService]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				svc.SetKonnectID(id)
				SetKonnectEntityProgrammedCondition(svc)

				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongService]{
					Op:  UpdateOp,
					Err: sdkError,
				}
			}
		}

		SetKonnectEntityProgrammedConditionFalse(svc, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(svc)

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
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, svc); errWrap != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
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
			Err: errWrap,
		}
	}

	return nil
}

func kongServiceToSDKServiceInput(
	svc *configurationv1alpha1.KongService,
) sdkkonnectcomp.ServiceInput {
	return sdkkonnectcomp.ServiceInput{
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
		Tags:           GenerateTagsForObject(svc, svc.Spec.KongServiceAPISpec.Tags...),
		TLSVerify:      svc.Spec.KongServiceAPISpec.TLSVerify,
		TLSVerifyDepth: svc.Spec.KongServiceAPISpec.TLSVerifyDepth,
		WriteTimeout:   svc.Spec.KongServiceAPISpec.WriteTimeout,
	}
}
