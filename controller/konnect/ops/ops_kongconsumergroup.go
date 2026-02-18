package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"

	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

func createConsumerGroup(
	ctx context.Context,
	sdk sdkops.ConsumerGroupSDK,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	if group.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: group, Op: CreateOp}
	}

	resp, err := sdk.CreateConsumerGroup(ctx,
		group.Status.Konnect.ControlPlaneID,
		kongConsumerGroupToSDKConsumerGroupInput(group),
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, group); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ConsumerGroup == nil || resp.ConsumerGroup.ID == nil || *resp.ConsumerGroup.ID == "" {
		return fmt.Errorf("failed creating %s: %w", group.GetTypeName(), ErrNilResponse)
	}

	id := *resp.ConsumerGroup.ID
	group.SetKonnectID(id)

	return nil
}

// updateConsumerGroup updates a KongConsumerGroup in Konnect.
// The KongConsumerGroup is assumed to have a Konnect ID set in status.
// It returns an error if the KongConsumerGroup does not have a ControlPlaneRef.
func updateConsumerGroup(
	ctx context.Context,
	sdk sdkops.ConsumerGroupSDK,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	cpID := group.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: group, Op: UpdateOp}
	}

	_, err := sdk.UpsertConsumerGroup(ctx,
		sdkkonnectops.UpsertConsumerGroupRequest{
			ControlPlaneID:  cpID,
			ConsumerGroupID: group.GetKonnectStatus().GetKonnectID(),
			ConsumerGroup:   kongConsumerGroupToSDKConsumerGroupInput(group),
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, group); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteConsumerGroup deletes a KongConsumerGroup in Konnect.
// The KongConsumerGroup is assumed to have a Konnect ID set in status.
// It returns an error if the operation fails.
func deleteConsumerGroup(
	ctx context.Context,
	sdk sdkops.ConsumerGroupSDK,
	consumer *configurationv1beta1.KongConsumerGroup,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteConsumerGroup(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrap != nil {
		return handleDeleteError(ctx, err, consumer)
	}

	return nil
}

func kongConsumerGroupToSDKConsumerGroupInput(
	group *configurationv1beta1.KongConsumerGroup,
) sdkkonnectcomp.ConsumerGroup {
	return sdkkonnectcomp.ConsumerGroup{
		Tags: GenerateTagsForObject(group, group.Spec.Tags...),
		Name: group.Spec.Name,
	}
}

// getKongConsumerGroupForUID lists consumer groups in Konnect with given k8s uid as its tag.
func getKongConsumerGroupForUID(
	ctx context.Context,
	sdk sdkops.ConsumerGroupSDK,
	cg *configurationv1beta1.KongConsumerGroup,
) (string, error) {
	cpID := cg.GetControlPlaneID()

	reqList := sdkkonnectops.ListConsumerGroupRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(cg)),
	}

	resp, err := sdk.ListConsumerGroup(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cg.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cg.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cg)
}
