package ops

import (
	"context"
	"errors"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createKongCredentialAPIKey(
	ctx context.Context,
	sdk KongCredentialAPIKeySDK,
	cred *configurationv1alpha1.KongCredentialAPIKey,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: CreateOp}
	}

	resp, err := sdk.CreateKeyAuthWithConsumer(ctx,
		sdkkonnectops.CreateKeyAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			KeyAuthWithoutParents:       kongCredentialAPIKeyToKeyAuthWithoutParents(cred),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cred); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cred.Status.Konnect.SetKonnectID(*resp.KeyAuth.ID)
	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// updateKongCredentialAPIKey updates the Konnect BasicAuth entity.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the BasicAuth does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialAPIKey(
	ctx context.Context,
	sdk KongCredentialAPIKeySDK,
	cred *configurationv1alpha1.KongCredentialAPIKey,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: UpdateOp}
	}

	_, err := sdk.UpsertKeyAuthWithConsumer(ctx,
		sdkkonnectops.UpsertKeyAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			KeyAuthID:                   cred.GetKonnectStatus().GetKonnectID(),
			KeyAuthWithoutParents:       kongCredentialAPIKeyToKeyAuthWithoutParents(cred),
		})

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// deleteKongCredentialAPIKey deletes a BasicAuth credential in Konnect.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialAPIKey(
	ctx context.Context,
	sdk KongCredentialAPIKeySDK,
	cred *configurationv1alpha1.KongCredentialAPIKey,
) error {
	cpID := cred.GetControlPlaneID()
	id := cred.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteKeyAuthWithConsumer(ctx,
		sdkkonnectops.DeleteKeyAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			KeyAuthID:                   id,
		})
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cred); errWrap != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", cred.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongCredentialAPIKey]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongService]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func kongCredentialAPIKeyToKeyAuthWithoutParents(
	cred *configurationv1alpha1.KongCredentialAPIKey,
) sdkkonnectcomp.KeyAuthWithoutParents {
	return sdkkonnectcomp.KeyAuthWithoutParents{
		Key:  lo.ToPtr(cred.Spec.Key),
		Tags: GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
}
