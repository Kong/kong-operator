package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func createKongCredentialBasicAuth(
	ctx context.Context,
	sdk sdkkonnectgo.BasicAuthCredentialsSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: CreateOp}
	}

	resp, err := sdk.CreateBasicAuthWithConsumer(ctx,
		sdkkonnectops.CreateBasicAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			BasicAuthWithoutParents:     kongCredentialBasicAuthToBasicAuthWithoutParents(cred),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cred); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.BasicAuth == nil || resp.BasicAuth.ID == nil {
		return fmt.Errorf("failed creating %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	cred.SetKonnectID(*resp.BasicAuth.ID)

	return nil
}

// updateKongCredentialBasicAuth updates the Konnect BasicAuth entity.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the BasicAuth does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialBasicAuth(
	ctx context.Context,
	sdk sdkkonnectgo.BasicAuthCredentialsSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: UpdateOp}
	}

	_, err := sdk.UpsertBasicAuthWithConsumer(ctx, sdkkonnectops.UpsertBasicAuthWithConsumerRequest{
		ControlPlaneID:              cpID,
		ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
		BasicAuthID:                 cred.GetKonnectStatus().GetKonnectID(),
		BasicAuthWithoutParents:     kongCredentialBasicAuthToBasicAuthWithoutParents(cred),
	})

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKongCredentialBasicAuth deletes a BasicAuth credential in Konnect.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialBasicAuth(
	ctx context.Context,
	sdk sdkkonnectgo.BasicAuthCredentialsSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	id := cred.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteBasicAuthWithConsumer(ctx,
		sdkkonnectops.DeleteBasicAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			BasicAuthID:                 id,
		})
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cred); errWrap != nil {
		return handleDeleteError(ctx, err, cred)
	}

	return nil
}

func adoptKongCredentialBasicAuth(
	ctx context.Context,
	sdk sdkkonnectgo.BasicAuthCredentialsSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return KonnectEntityAdoptionMissingControlPlaneIDError{}
	}
	if cred.Status.Konnect == nil || cred.Status.Konnect.GetConsumerID() == "" {
		return fmt.Errorf("can't adopt %T %s without a Konnect Consumer ID", cred, client.ObjectKeyFromObject(cred))
	}
	if cred.Spec.Adopt == nil || cred.Spec.Adopt.Konnect == nil {
		return fmt.Errorf("missing Konnect adoption options for %T %s", cred, client.ObjectKeyFromObject(cred))
	}

	adoptOptions := cred.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetBasicAuthWithConsumer(ctx, sdkkonnectops.GetBasicAuthWithConsumerRequest{
		ControlPlaneID:              cpID,
		ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
		BasicAuthID:                 konnectID,
	})
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.BasicAuth == nil {
		return fmt.Errorf("failed to adopt %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.BasicAuth.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(cred.UID) {
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
		credCopy := cred.DeepCopy()
		credCopy.SetKonnectID(konnectID)
		if err = updateKongCredentialBasicAuth(ctx, sdk, credCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !credentialBasicAuthMatch(resp.BasicAuth, cred) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	cred.SetKonnectID(konnectID)
	return nil
}

func kongCredentialBasicAuthToBasicAuthWithoutParents(
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) sdkkonnectcomp.BasicAuthWithoutParents {
	return sdkkonnectcomp.BasicAuthWithoutParents{
		Password: cred.Spec.Password,
		Username: cred.Spec.Username,
		Tags:     GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
}

// getKongCredentialBasicAuthForUID lists basic auth credentials in Konnect with given k8s uid as its tag.
func getKongCredentialBasicAuthForUID(
	ctx context.Context,
	sdk sdkkonnectgo.BasicAuthCredentialsSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) (string, error) {
	cpID := cred.GetControlPlaneID()

	req := sdkkonnectops.ListBasicAuthRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           new(UIDLabelForObject(cred)),
	}

	resp, err := sdk.ListBasicAuth(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cred)
}

func credentialBasicAuthMatch(
	konnectBasicAuth *sdkkonnectcomp.BasicAuth,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) bool {
	if konnectBasicAuth == nil {
		return false
	}

	// TODO(pmalek): Password field is write only and is not returned https://github.com/Kong/kong-operator/issues/2535
	return konnectBasicAuth.Username == cred.Spec.Username
}
