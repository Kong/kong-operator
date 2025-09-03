package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// createKeySet creates a KongKeySet in Konnect.
// It sets the KonnectID in the KongKeySet status.
func createKeySet(
	ctx context.Context,
	sdk sdkops.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) error {
	cpID := keySet.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: keySet}
	}

	resp, err := sdk.CreateKeySet(ctx,
		cpID,
		kongKeySetToKeySetInput(keySet),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, keySet); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.KeySet == nil || resp.KeySet.ID == nil || *resp.KeySet.ID == "" {
		return fmt.Errorf("failed creating %s: %w", keySet.GetTypeName(), ErrNilResponse)
	}

	// At this point, the KeySet has been created successfully.
	keySet.SetKonnectID(*resp.KeySet.ID)

	return nil
}

// updateKeySet updates a KongKeySet in Konnect.
// The KongKeySet must have a KonnectID set in its status.
// It returns an error if the KongKeySet does not have a KonnectID.
func updateKeySet(
	ctx context.Context,
	sdk sdkops.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) error {
	cpID := keySet.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: keySet, Op: UpdateOp}
	}

	_, err := sdk.UpsertKeySet(ctx,
		sdkkonnectops.UpsertKeySetRequest{
			ControlPlaneID: cpID,
			KeySetID:       keySet.GetKonnectStatus().GetKonnectID(),
			KeySet:         kongKeySetToKeySetInput(keySet),
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, keySet); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKeySet deletes a KongKeySet in Konnect.
// The KongKeySet must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteKeySet(
	ctx context.Context,
	sdk sdkops.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) error {
	id := keySet.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteKeySet(ctx, keySet.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, keySet); errWrap != nil {
		return handleDeleteError(ctx, err, keySet)
	}

	return nil
}

func kongKeySetToKeySetInput(keySet *configurationv1alpha1.KongKeySet) *sdkkonnectcomp.KeySet {
	return &sdkkonnectcomp.KeySet{
		Name: lo.ToPtr(keySet.Spec.Name),
		Tags: GenerateTagsForObject(keySet, keySet.Spec.Tags...),
	}
}

func getKongKeySetForUID(
	ctx context.Context,
	sdk sdkops.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) (string, error) {
	resp, err := sdk.ListKeySet(ctx, sdkkonnectops.ListKeySetRequest{
		ControlPlaneID: keySet.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(keySet)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list KongKeySets: %w", err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", keySet.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), keySet)
}
