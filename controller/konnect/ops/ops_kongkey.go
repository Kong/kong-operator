package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

// createKey creates a KongKey in Konnect.
// It sets the KonnectID in the KongKey status.
func createKey(
	ctx context.Context,
	sdk sdkkonnectgo.KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	cpID := key.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: key, Op: CreateOp}
	}

	resp, err := sdk.CreateKey(ctx,
		cpID,
		kongKeyToKeyInput(key),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, key); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Key == nil || resp.Key.ID == nil || *resp.Key.ID == "" {
		return fmt.Errorf("failed creating %s: %w", key.GetTypeName(), ErrNilResponse)
	}

	key.SetKonnectID(*resp.Key.ID)

	return nil
}

// updateKey updates a KongKey in Konnect.
// The KongKey must have a KonnectID set in its status.
// It returns an error if the KongKey does not have a KonnectID.
func updateKey(
	ctx context.Context,
	sdk sdkkonnectgo.KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	cpID := key.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: key, Op: UpdateOp}
	}

	_, err := sdk.UpsertKey(ctx,
		sdkkonnectops.UpsertKeyRequest{
			ControlPlaneID: cpID,
			KeyID:          key.GetKonnectStatus().GetKonnectID(),
			Key:            kongKeyToKeyInput(key),
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, key); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKey deletes a KongKey in Konnect.
// The KongKey must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteKey(
	ctx context.Context,
	sdk sdkkonnectgo.KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	id := key.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteKey(ctx, key.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, key); errWrap != nil {
		return handleDeleteError(ctx, err, key)
	}

	return nil
}

func adoptKey(
	ctx context.Context,
	sdk sdkkonnectgo.KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	cpID := key.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}

	adoptOptions := key.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetKey(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}

	if resp == nil || resp.Key == nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       fmt.Errorf("empty response when fetching key"),
		}
	}

	uidTag, hasUIDTag := findUIDTag(resp.Key.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(key.UID) {
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
		keyCopy := key.DeepCopy()
		keyCopy.SetKonnectID(konnectID)
		if err = updateKey(ctx, sdk, keyCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !keyMatch(resp.Key, key) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	key.SetKonnectID(konnectID)

	if adoptMode == commonv1alpha1.AdoptModeMatch {
		actualKeySetID := ""
		if resp.Key.Set != nil && resp.Key.Set.ID != nil {
			actualKeySetID = lo.FromPtr(resp.Key.Set.ID)
		}
		key.Status.Konnect.SetKeySetID(actualKeySetID)
	}

	return nil
}

func kongKeyToKeyInput(key *configurationv1alpha1.KongKey) sdkkonnectcomp.Key {
	k := sdkkonnectcomp.Key{
		Jwk:  key.Spec.JWK,
		Kid:  key.Spec.KID,
		Name: key.Spec.Name,
		Tags: GenerateTagsForObject(key, key.Spec.Tags...),
	}
	if key.Spec.PEM != nil {
		k.Pem = &sdkkonnectcomp.Pem{
			PrivateKey: lo.ToPtr(key.Spec.PEM.PrivateKey),
			PublicKey:  lo.ToPtr(key.Spec.PEM.PublicKey),
		}
	}
	if konnectStatus := key.Status.Konnect; konnectStatus != nil {
		if keySetID := konnectStatus.GetKeySetID(); keySetID != "" {
			k.Set = &sdkkonnectcomp.Set{
				ID: lo.ToPtr(konnectStatus.GetKeySetID()),
			}
		}
	}
	return k
}

func getKongKeyForUID(
	ctx context.Context,
	sdk sdkkonnectgo.KeysSDK,
	key *configurationv1alpha1.KongKey,
) (string, error) {
	resp, err := sdk.ListKey(ctx, sdkkonnectops.ListKeyRequest{
		ControlPlaneID: key.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(key)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list KongKeys: %w", err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed to list KongKeys: %w", ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), key)
}

func keyMatch(konnectKey *sdkkonnectcomp.Key, key *configurationv1alpha1.KongKey) bool {
	if konnectKey == nil {
		return false
	}

	if konnectKey.GetKid() != key.Spec.KID {
		return false
	}

	if !equalWithDefault(konnectKey.Name, key.Spec.Name, "") {
		return false
	}

	if key.Spec.JWK != nil || konnectKey.Jwk != nil {
		if !equalWithDefault(konnectKey.Jwk, key.Spec.JWK, "") {
			return false
		}
	}

	if key.Spec.PEM != nil {
		if konnectKey.Pem == nil ||
			konnectKey.Pem.PublicKey == nil ||
			*konnectKey.Pem.PublicKey != key.Spec.PEM.PublicKey {
			return false
		}
		// Private key may not be returned by Konnect. If it is returned, ensure it matches.
		if konnectKey.Pem.PrivateKey != nil && *konnectKey.Pem.PrivateKey != key.Spec.PEM.PrivateKey {
			return false
		}
	} else if konnectKey.Pem != nil && konnectKey.Pem.PublicKey != nil && key.Spec.JWK == nil {
		// If spec does not expect a PEM or JWK, but Konnect returns a PEM, treat as mismatch.
		return false
	}

	expectedKeySetID := ""
	if key.Status.Konnect != nil {
		expectedKeySetID = key.Status.Konnect.GetKeySetID()
	}
	actualKeySetID := ""
	if konnectKey.Set != nil && konnectKey.Set.ID != nil {
		actualKeySetID = lo.FromPtr(konnectKey.Set.ID)
	}
	return expectedKeySetID == actualKeySetID
}
