package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
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

func adoptService(
	ctx context.Context,
	sdk sdkops.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	cpID := svc.GetControlPlaneID()
	konnectID := svc.Spec.Adopt.Konnect.ID
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}
	resp, err := sdk.GetService(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	uidTag, hasUIDTag := findUIDTag(resp.Service.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(svc.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	svcCopy := svc.DeepCopy()
	svcCopy.SetKonnectID(konnectID)
	if err = updateService(ctx, sdk, svcCopy); err != nil {
		return err
	}

	svc.SetKonnectID(konnectID)
	return nil
}

func kongServiceToSDKServiceInput(
	svc *configurationv1alpha1.KongService,
) sdkkonnectcomp.Service {
	s := sdkkonnectcomp.Service{
		URL:            svc.Spec.URL,
		ConnectTimeout: svc.Spec.ConnectTimeout,
		Enabled:        svc.Spec.Enabled,
		Host:           svc.Spec.Host,
		Name:           svc.Spec.Name,
		Path:           svc.Spec.Path,
		ReadTimeout:    svc.Spec.ReadTimeout,
		Retries:        svc.Spec.Retries,
		Tags:           GenerateTagsForObject(svc, svc.Spec.Tags...),
		TLSVerify:      svc.Spec.TLSVerify,
		TLSVerifyDepth: svc.Spec.TLSVerifyDepth,
		WriteTimeout:   svc.Spec.WriteTimeout,
	}
	if svc.Spec.Port != 0 {
		s.Port = lo.ToPtr(svc.Spec.Port)
	}
	if svc.Spec.Protocol != "" {
		s.Protocol = lo.ToPtr(svc.Spec.Protocol)
	}
	return s
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

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), svc)
}
