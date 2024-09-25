package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

// createKey creates a KongKey in Konnect.
// It sets the KonnectID and the Programmed condition in the KongKey status.
func createKey(
	ctx context.Context,
	sdk KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	cpID := key.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", key, client.ObjectKeyFromObject(key))
	}

	resp, err := sdk.CreateKey(ctx,
		cpID,
		kongKeyToKeyInput(key),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, key); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(key, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	key.Status.Konnect.SetKonnectID(*resp.Key.ID)
	SetKonnectEntityProgrammedCondition(key)

	return nil
}

// updateKey updates a KongKey in Konnect.
// The KongKey must have a KonnectID set in its status.
// It returns an error if the KongKey does not have a KonnectID.
func updateKey(
	ctx context.Context,
	sdk KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	cpID := key.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T without a ControlPlaneID", key)
	}

	_, err := sdk.UpsertKey(ctx,
		sdkkonnectops.UpsertKeyRequest{
			ControlPlaneID: cpID,
			KeyID:          key.GetKonnectStatus().GetKonnectID(),
			Key:            kongKeyToKeyInput(key),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, key); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(key, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(key)

	return nil
}

// deleteKey deletes a KongKey in Konnect.
// The KongKey must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteKey(
	ctx context.Context,
	sdk KeysSDK,
	key *configurationv1alpha1.KongKey,
) error {
	id := key.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteKey(ctx, key.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, key); errWrap != nil {
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", key.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongKey]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongKey]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func kongKeyToKeyInput(key *configurationv1alpha1.KongKey) sdkkonnectcomp.KeyInput {
	var (
		annotationTags = metadata.ExtractTags(key)
		specTags       = key.Spec.Tags
		k8sMetaTags    = GenerateKubernetesMetadataTags(key)
	)
	k := sdkkonnectcomp.KeyInput{
		Jwk:  key.Spec.JWK,
		Kid:  key.Spec.KID,
		Name: key.Spec.Name,
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: lo.Uniq(slices.Concat(annotationTags, specTags, k8sMetaTags)),
	}
	if key.Spec.PEM != nil {
		k.Pem = &sdkkonnectcomp.Pem{
			PrivateKey: lo.ToPtr(key.Spec.PEM.PrivateKey),
			PublicKey:  lo.ToPtr(key.Spec.PEM.PublicKey),
		}
	}
	return k
}
