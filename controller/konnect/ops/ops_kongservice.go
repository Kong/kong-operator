package ops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

func createService(
	ctx context.Context,
	sdk sdkkonnectgo.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	if svc.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: svc, Op: CreateOp}
	}

	if svc.Spec.Name == nil || *svc.Spec.Name == "" {
		existingID, err := getKongServiceForUID(ctx, sdk, svc)
		if err == nil {
			svc.SetKonnectID(existingID)
			return nil
		}
		var notFound EntityWithMatchingUIDNotFoundError
		if !errors.As(err, &notFound) {
			return err
		}
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
	sdk sdkkonnectgo.ServicesSDK,
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
	sdk sdkkonnectgo.ServicesSDK,
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
	sdk sdkkonnectgo.ServicesSDK,
	svc *configurationv1alpha1.KongService,
) error {
	cpID := svc.GetControlPlaneID()
	adoptOptions := svc.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID
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

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}
	switch adoptOptions.Mode {
	case commonv1alpha1.AdoptModeOverride:
		svcCopy := svc.DeepCopy()
		svcCopy.SetKonnectID(konnectID)
		if err = updateService(ctx, sdk, svcCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		// When adopting in match mode, we return error if the service does not match.
		// when it matches, we do nothing but fill the Konnect ID to mark that the adoption is successful.
		if !serviceMatch(resp.Service, svc) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
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
	sdk sdkkonnectgo.ServicesSDK,
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

// serviceMatch compares the existing service fetched from Konnect and the spec of the KongService
// for adopting in match mode.
func serviceMatch(konnectService *sdkkonnectcomp.ServiceOutput, svc *configurationv1alpha1.KongService) bool {
	spec := svc.Spec
	if spec.URL != nil {
		parsedURL, err := url.Parse(*spec.URL)
		if err != nil {
			return false
		}
		spec.Protocol = sdkkonnectcomp.Protocol(parsedURL.Scheme)
		spec.Host = parsedURL.Hostname()
		spec.Port, _ = strconv.ParseInt(parsedURL.Port(), 10, 64)
		spec.Path = lo.ToPtr(parsedURL.Path)
	}
	return equalWithDefault(konnectService.Name, spec.Name, "") &&
		konnectService.Host == spec.Host &&
		equalWithDefault(konnectService.Port, &spec.Port, 80) &&
		equalWithDefault(konnectService.Protocol, &spec.Protocol, "http") &&
		equalWithDefault(konnectService.Path, spec.Path, "/")
}
