package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
)

func createConsumerGroup(
	ctx context.Context,
	sdk sdkkonnectgo.ConsumerGroupsSDK,
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
	sdk sdkkonnectgo.ConsumerGroupsSDK,
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
	sdk sdkkonnectgo.ConsumerGroupsSDK,
	consumer *configurationv1beta1.KongConsumerGroup,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteConsumerGroup(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrap != nil {
		return handleDeleteError(ctx, err, consumer)
	}

	return nil
}

func adoptConsumerGroup(
	ctx context.Context,
	sdk sdkkonnectgo.ConsumerGroupsSDK,
	group *configurationv1beta1.KongConsumerGroup,
	adoptOptions commonv1alpha1.AdoptOptions,
) error {
	cpID := group.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: group, Op: AdoptOp}
	}
	konnectID := adoptOptions.Konnect.ID
	resp, err := sdk.GetConsumerGroup(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}

	if resp == nil || resp.GetConsumerGroupInsideWrapper() == nil || resp.GetConsumerGroupInsideWrapper().GetConsumerGroup() == nil {
		return fmt.Errorf("failed to adopt %s: %w", group.GetTypeName(), ErrNilResponse)
	}

	existing := resp.GetConsumerGroupInsideWrapper().GetConsumerGroup()

	uidTag, hasUIDTag := findUIDTag(existing.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(group.UID) {
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
		groupCopy := group.DeepCopy()
		groupCopy.SetKonnectID(konnectID)
		if err = updateConsumerGroup(ctx, sdk, groupCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !consumerGroupMatch(existing, group) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	group.SetKonnectID(konnectID)
	return nil
}

func consumerGroupMatch(
	existing *sdkkonnectcomp.ConsumerGroup,
	group *configurationv1beta1.KongConsumerGroup,
) bool {
	if existing == nil {
		return false
	}

	return existing.GetName() == group.Spec.Name
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
	sdk sdkkonnectgo.ConsumerGroupsSDK,
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
