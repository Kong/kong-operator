package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// createKeySet creates a KongKeySet in Konnect.
// It sets the KonnectID in the KongKeySet status.
func createKeySet(
	ctx context.Context,
	sdk sdkkonnectgo.KeySetsSDK,
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
	sdk sdkkonnectgo.KeySetsSDK,
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
	sdk sdkkonnectgo.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) error {
	id := keySet.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteKeySet(ctx, keySet.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, keySet); errWrap != nil {
		return handleDeleteError(ctx, err, keySet)
	}

	return nil
}

func adoptKeySet(
	ctx context.Context,
	sdk sdkkonnectgo.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) error {
	cpID := keySet.GetControlPlaneID()
	if cpID == "" {
		return KonnectEntityAdoptionMissingControlPlaneIDError{}
	}

	adoptOptions := keySet.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetKeySet(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}

	if resp == nil || resp.KeySet == nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       fmt.Errorf("empty response when fetching key set"),
		}
	}

	uidTag, hasUIDTag := findUIDTag(resp.KeySet.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(keySet.UID) {
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
		keySetCopy := keySet.DeepCopy()
		keySetCopy.SetKonnectID(konnectID)
		if err = updateKeySet(ctx, sdk, keySetCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !keySetMatch(resp.KeySet, keySet) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	keySet.SetKonnectID(konnectID)
	return nil
}

func kongKeySetToKeySetInput(keySet *configurationv1alpha1.KongKeySet) *sdkkonnectcomp.KeySet {
	return &sdkkonnectcomp.KeySet{
		Name: new(keySet.Spec.Name),
		Tags: GenerateTagsForObject(keySet, keySet.Spec.Tags...),
	}
}

func getKongKeySetForUID(
	ctx context.Context,
	sdk sdkkonnectgo.KeySetsSDK,
	keySet *configurationv1alpha1.KongKeySet,
) (string, error) {
	resp, err := sdk.ListKeySet(ctx, sdkkonnectops.ListKeySetRequest{
		ControlPlaneID: keySet.GetControlPlaneID(),
		Tags:           new(UIDLabelForObject(keySet)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list KongKeySets: %w", err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", keySet.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), keySet)
}

func keySetMatch(konnectKeySet *sdkkonnectcomp.KeySet, keySet *configurationv1alpha1.KongKeySet) bool {
	if konnectKeySet == nil {
		return false
	}

	if !equalWithDefault(konnectKeySet.Name, new(keySet.Spec.Name), "") {
		return false
	}

	for _, tag := range keySet.Spec.Tags {
		if !lo.Contains(konnectKeySet.Tags, tag) {
			return false
		}
	}

	return true
}
