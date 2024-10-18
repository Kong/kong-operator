package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createTarget(
	ctx context.Context,
	sdk TargetsSDK,
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
		SetKonnectEntityProgrammedConditionFalse(target, "FailedToCreate", errWrapped.Error())
		return errWrapped
	}

	target.Status.Konnect.SetKonnectID(*resp.Target.ID)
	SetKonnectEntityProgrammedCondition(target)

	return nil
}

func updateTarget(
	ctx context.Context,
	sdk TargetsSDK,
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
		SetKonnectEntityProgrammedConditionFalse(target, "FailedToUpdate", errWrapped.Error())
		return errWrapped
	}

	SetKonnectEntityProgrammedCondition(target)
	return nil
}

func deleteTarget(
	ctx context.Context,
	sdk TargetsSDK,
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
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			if sdkError.StatusCode == http.StatusNotFound {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", target.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongTarget]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongTarget]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
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
