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

func createKongCredentialACL(
	ctx context.Context,
	sdk KongCredentialACLSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", cred, client.ObjectKeyFromObject(cred))
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
		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cred.Status.Konnect.SetKonnectID(*resp.ACL.ID)
	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// updateKongCredentialACL updates the Konnect ACL entity.
// It is assumed that the provided ACL has Konnect ID set in status.
// It returns an error if the ACL does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialACL(
	ctx context.Context,
	sdk KongCredentialACLSDK,
	cred *configurationv1alpha1.KongCredentialACL,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect ControlPlane ID", cred, client.ObjectKeyFromObject(cred))
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
		// ACL update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				if err := createKongCredentialACL(ctx, sdk, cred); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongCredentialACL]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongCredentialACL]{
					Op:  UpdateOp,
					Err: sdkError,
				}

			}
		}

		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// deleteKongCredentialACL deletes an ACL credential in Konnect.
// It is assumed that the provided ACL has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialACL(
	ctx context.Context,
	sdk KongCredentialACLSDK,
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
			return FailedKonnectOpError[configurationv1alpha1.KongCredentialACL]{
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

func kongCredentialACLToACLWithoutParents(
	cred *configurationv1alpha1.KongCredentialACL,
) sdkkonnectcomp.ACLWithoutParents {
	var (
		specTags       = cred.Spec.Tags
		annotationTags = metadata.ExtractTags(cred)
		k8sTags        = GenerateKubernetesMetadataTags(cred)
	)
	// Deduplicate tags to avoid rejection by Konnect.
	tags := lo.Uniq(slices.Concat(specTags, annotationTags, k8sTags))

	return sdkkonnectcomp.ACLWithoutParents{
		Group: lo.ToPtr(cred.Spec.Group),
		Tags:  tags,
	}
}
