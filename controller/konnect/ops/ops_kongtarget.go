package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createTarget(
	ctx context.Context,
	sdk sdkops.TargetsSDK,
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
	sdk sdkops.TargetsSDK,
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
	sdk sdkops.TargetsSDK,
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

func kongTargetToTargetWithoutParents(target *configurationv1alpha1.KongTarget) sdkkonnectcomp.TargetWithoutParents {
	return sdkkonnectcomp.TargetWithoutParents{
		Target: lo.ToPtr(target.Spec.Target),
		Weight: lo.ToPtr(int64(target.Spec.Weight)),
		Tags:   GenerateTagsForObject(target, target.Spec.Tags...),
	}
}

// getKongTargetForUID returns the Konnect ID of the KongTarget
// that matches the UID of the provided KongTarget.
func getKongTargetForUID(
	ctx context.Context,
	sdk sdkops.TargetsSDK,
	target *configurationv1alpha1.KongTarget,
) (string, error) {
	reqList := sdkkonnectops.ListTargetWithUpstreamRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		Tags:           lo.ToPtr(UIDLabelForObject(target)),
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
