package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func createTarget(
	ctx context.Context,
	sdk sdkkonnectgo.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) error {
	cpID := target.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: target, Op: CreateOp}
	}

	if target.Status.Konnect == nil || target.Status.Konnect.UpstreamID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect Upstream ID", target, client.ObjectKeyFromObject(target))
	}

	resp, err := sdk.CreateTargetWithUpstream(ctx, sdkkonnectops.CreateTargetWithUpstreamRequest{
		ControlPlaneID:       cpID,
		UpstreamIDForTarget:  target.Status.Konnect.UpstreamID,
		TargetWithoutParents: kongTargetToTargetWithoutParents(target),
	})

	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, target); errWrapped != nil {
		return errWrapped
	}

	if resp == nil || resp.Target == nil || resp.Target.ID == nil {
		return fmt.Errorf("failed creating %s: %w", target.GetTypeName(), ErrNilResponse)
	}

	target.SetKonnectID(*resp.Target.ID)

	return nil
}

func updateTarget(
	ctx context.Context,
	sdk sdkkonnectgo.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) error {
	cpID := target.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: target, Op: UpdateOp}
	}
	if target.Status.Konnect == nil || target.Status.Konnect.UpstreamID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect Upstream ID", target, client.ObjectKeyFromObject(target))
	}

	_, err := sdk.UpsertTargetWithUpstream(ctx, sdkkonnectops.UpsertTargetWithUpstreamRequest{
		ControlPlaneID:       cpID,
		UpstreamIDForTarget:  target.Status.Konnect.UpstreamID,
		TargetID:             target.GetKonnectID(),
		TargetWithoutParents: kongTargetToTargetWithoutParents(target),
	})

	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, target); errWrapped != nil {
		return errWrapped
	}

	return nil
}

func deleteTarget(
	ctx context.Context,
	sdk sdkkonnectgo.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) error {
	cpID := target.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't delete %T %s without a Konnect ControlPlane ID", target, client.ObjectKeyFromObject(target))
	}
	if target.Status.Konnect == nil || target.Status.Konnect.UpstreamID == "" {
		return fmt.Errorf("can't delete %T %s without a Konnect Upstream ID", target, client.ObjectKeyFromObject(target))
	}
	id := target.GetKonnectID()

	_, err := sdk.DeleteTargetWithUpstream(ctx, sdkkonnectops.DeleteTargetWithUpstreamRequest{
		ControlPlaneID:      cpID,
		UpstreamIDForTarget: target.Status.Konnect.UpstreamID,
		TargetID:            id,
	})

	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, target); errWrapped != nil {
		return handleDeleteError(ctx, err, target)
	}

	return nil
}

func adoptTarget(
	ctx context.Context,
	sdk sdkkonnectgo.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) error {
	cpID := target.GetControlPlaneID()

	if cpID == "" {
		return KonnectEntityAdoptionMissingControlPlaneIDError{}
	}
	if target.Status.Konnect == nil || target.Status.Konnect.UpstreamID == "" {
		return fmt.Errorf("can't adopt %T %s without a Konnect Upstream ID", target, client.ObjectKeyFromObject(target))
	}
	adoptOptions := target.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetTargetWithUpstream(ctx, sdkkonnectops.GetTargetWithUpstreamRequest{
		ControlPlaneID:      cpID,
		UpstreamIDForTarget: target.Status.Konnect.UpstreamID,
		TargetID:            konnectID,
	})
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	uidTag, hasUIDTag := findUIDTag(resp.Target.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(target.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}
	switch adoptMode {
	case commonv1alpha1.AdoptModeOverride:
		targetCopy := target.DeepCopy()
		targetCopy.SetKonnectID(konnectID)
		if err = updateTarget(ctx, sdk, targetCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		// When adopting in match mode, we return error if the upstream does not match.
		// when it matches, we do nothing but fill the Konnect ID to mark that the adoption is successful.
		if !targetMatch(resp.Target, target) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	target.SetKonnectID(konnectID)
	return nil
}

func kongTargetToTargetWithoutParents(target *configurationv1alpha1.KongTarget) sdkkonnectcomp.TargetWithoutParents {
	return sdkkonnectcomp.TargetWithoutParents{
		Target: new(target.Spec.Target),
		Weight: new(int64(target.Spec.Weight)),
		Tags:   GenerateTagsForObject(target, target.Spec.Tags...),
	}
}

// getKongTargetForUID returns the Konnect ID of the KongTarget
// that matches the UID of the provided KongTarget.
func getKongTargetForUID(
	ctx context.Context,
	sdk sdkkonnectgo.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) (string, error) {
	reqList := sdkkonnectops.ListTargetWithUpstreamRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		Tags:           new(UIDLabelForObject(target)),
		ControlPlaneID: target.GetControlPlaneID(),
	}

	resp, err := sdk.ListTargetWithUpstream(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", target.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", target.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), target)
}

func targetMatch(konnectTarget *sdkkonnectcomp.Target, target *configurationv1alpha1.KongTarget) bool {
	return equalWithDefault(konnectTarget.Target, &target.Spec.Target, "") &&
		equalWithDefault(konnectTarget.Weight, new(int64(target.Spec.Weight)), 100)
}
