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

func createKongCredentialAPIKey(
	ctx context.Context,
	sdk sdkops.KongCredentialAPIKeySDK,
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
		return errWrap
	}

	if resp == nil || resp.KeyAuth == nil || resp.KeyAuth.ID == nil {
		return fmt.Errorf("failed creating %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	cred.SetKonnectID(*resp.KeyAuth.ID)

	return nil
}

// updateKongCredentialAPIKey updates the Konnect BasicAuth entity.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the BasicAuth does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialAPIKey(
	ctx context.Context,
	sdk sdkops.KongCredentialAPIKeySDK,
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

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKongCredentialAPIKey deletes a BasicAuth credential in Konnect.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialAPIKey(
	ctx context.Context,
	sdk sdkops.KongCredentialAPIKeySDK,
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
		return handleDeleteError(ctx, err, cred)
	}

	return nil
}

func kongCredentialAPIKeyToKeyAuthWithoutParents(
	cred *configurationv1alpha1.KongCredentialAPIKey,
) *sdkkonnectcomp.KeyAuthWithoutParents {
	return &sdkkonnectcomp.KeyAuthWithoutParents{
		// Key is required in CRD so we don't need to check if it has been provided.
		Key:  lo.ToPtr(cred.Spec.Key),
		Tags: GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
}

// getKongCredentialAPIKeyForUID lists API key credentials in Konnect with given k8s uid as its tag.
func getKongCredentialAPIKeyForUID(
	ctx context.Context,
	sdk sdkops.KongCredentialAPIKeySDK,
	cred *configurationv1alpha1.KongCredentialAPIKey,
) (string, error) {
	cpID := cred.GetControlPlaneID()

	req := sdkkonnectops.ListKeyAuthRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(cred)),
	}

	resp, err := sdk.ListKeyAuth(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cred)
}
