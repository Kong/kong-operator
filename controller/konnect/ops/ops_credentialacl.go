package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

func createKongCredentialACL(
	ctx context.Context,
	sdk sdkkonnectgo.ACLsSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: CreateOp}
	}

	resp, err := sdk.CreateACLWithConsumer(ctx,
		sdkkonnectops.CreateACLWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			ACLWithoutParents:           kongCredentialACLToACLWithoutParents(cred),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cred); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ACL == nil || resp.ACL.ID == nil {
		return fmt.Errorf("failed creating %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	cred.SetKonnectID(*resp.ACL.ID)

	return nil
}

// updateKongCredentialACL updates the Konnect ACL entity.
// It is assumed that the provided ACL has Konnect ID set in status.
// It returns an error if the ACL does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialACL(
	ctx context.Context,
	sdk sdkkonnectgo.ACLsSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: UpdateOp}
	}

	_, err := sdk.UpsertACLWithConsumer(ctx,
		sdkkonnectops.UpsertACLWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			ACLID:                       cred.GetKonnectStatus().GetKonnectID(),
			ACLWithoutParents:           kongCredentialACLToACLWithoutParents(cred),
		})

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKongCredentialACL deletes an ACL credential in Konnect.
// It is assumed that the provided ACL has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialACL(
	ctx context.Context,
	sdk sdkkonnectgo.ACLsSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	id := cred.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteACLWithConsumer(ctx,
		sdkkonnectops.DeleteACLWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			ACLID:                       id,
		})
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cred); errWrap != nil {
		return handleDeleteError(ctx, err, cred)
	}

	return nil
}

func adoptKongCredentialACL(
	ctx context.Context,
	sdk sdkkonnectgo.ACLsSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}
	if cred.Status.Konnect == nil || cred.Status.Konnect.GetConsumerID() == "" {
		return fmt.Errorf("can't adopt %T %s without a Konnect Consumer ID", cred, client.ObjectKeyFromObject(cred))
	}
	if cred.Spec.Adopt == nil || cred.Spec.Adopt.Konnect == nil {
		return fmt.Errorf("missing Konnect adoption options for %T %s", cred, client.ObjectKeyFromObject(cred))
	}

	adoptOptions := cred.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetACLWithConsumer(ctx, sdkkonnectops.GetACLWithConsumerRequest{
		ControlPlaneID:              cpID,
		ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
		ACLID:                       konnectID,
	})
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.ACL == nil {
		return fmt.Errorf("failed to adopt %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.ACL.Tags)
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
		if err = updateKongCredentialACL(ctx, sdk, credCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !credentialACLMatch(resp.ACL, cred) {
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

func kongCredentialACLToACLWithoutParents(
	cred *configurationv1alpha1.KongCredentialACL,
) sdkkonnectcomp.ACLWithoutParents {
	return sdkkonnectcomp.ACLWithoutParents{
		Group: cred.Spec.Group,
		Tags:  GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
}

// getKongCredentialACLForUID lists API key credentials in Konnect with given k8s uid as its tag.
func getKongCredentialACLForUID(
	ctx context.Context,
	sdk sdkkonnectgo.ACLsSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) (string, error) {
	cpID := cred.GetControlPlaneID()

	req := sdkkonnectops.ListACLRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(cred)),
	}

	resp, err := sdk.ListACL(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), ErrNilResponse)
	}
	x := sliceToEntityWithIDPtrSlice(resp.Object.Data)

	return getMatchingEntryFromListResponseData(x, cred)
}

func credentialACLMatch(
	konnectACL *sdkkonnectcomp.ACL,
	cred *configurationv1alpha1.KongCredentialACL,
) bool {
	if konnectACL == nil {
		return false
	}

	return konnectACL.Group == cred.Spec.Group
}
