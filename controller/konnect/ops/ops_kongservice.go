package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createService(
	ctx context.Context,
	sdk sdkops.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: svc, Op: CreateOp}
	}

	resp, err := sdk.CreateService(ctx,
		svc.Status.Konnect.ControlPlaneID,
		kongServiceToSDKServiceInput(svc),
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, svc); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Service == nil || resp.Service.ID == nil {
		return fmt.Errorf("failed creating %s: %w", svc.GetTypeName(), ErrNilResponse)
	}

	svc.SetKonnectID(*resp.Service.ID)

	return nil
}

// updateService updates the Konnect Service entity.
// It is assumed that provided KongService has Konnect ID set in status.
// It returns an error if the KongService does not have a ControlPlaneRef or
// if the operation fails.
func updateService(
	ctx context.Context,
	sdk sdkops.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: svc, Op: UpdateOp}
	}

	id := svc.GetKonnectStatus().GetKonnectID()
	_, err := sdk.UpsertService(ctx,
		sdkkonnectops.UpsertServiceRequest{
			ControlPlaneID: svc.GetControlPlaneID(),
			ServiceID:      id,
			Service:        kongServiceToSDKServiceInput(svc),
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, svc); errWrap != nil {
		// Service update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				logEntityNotFoundRecreating(ctx, svc, id)
				if err := createService(ctx, sdk, svc); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongService]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				// Create succeeded, createService sets the status so no need to do this here.
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongService]{
					Op:  UpdateOp,
					Err: sdkError,
				}
			}
		}

		return errWrap
	}

	return nil
}

// deleteService deletes a KongService in Konnect.
// It is assumed that provided KongService has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteService(
	ctx context.Context,
	sdk sdkops.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	id := svc.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteService(ctx, svc.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, svc); errWrap != nil {
		return handleDeleteError(ctx, err, svc)
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

// getKongServiceForUID returns the Konnect ID of the KongService
// that matches the UID of the provided KongService.
func getKongServiceForUID(
	ctx context.Context,
	sdk sdkops.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) (string, error) {
	reqList := sdkkonnectops.ListServiceRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		Tags:           lo.ToPtr(UIDLabelForObject(svc)),
		ControlPlaneID: svc.GetControlPlaneID(),
	}

	resp, err := sdk.ListService(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", svc.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", svc.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.Object.Data), svc)
}
