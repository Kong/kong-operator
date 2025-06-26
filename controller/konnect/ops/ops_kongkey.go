package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// createKey creates a KongKey in Konnect.
// It sets the KonnectID in the KongKey status.
func createKey(
	ctx context.Context,
	sdk sdkops.KeysSDK,
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
	sdk sdkops.KeysSDK,
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
	sdk sdkops.KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	id := key.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteKey(ctx, key.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, key); errWrap != nil {
		return handleDeleteError(ctx, err, key)
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
	sdk sdkops.KeysSDK,
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
