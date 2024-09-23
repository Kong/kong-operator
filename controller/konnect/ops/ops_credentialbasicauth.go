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

func createKongCredentialBasicAuth(
	ctx context.Context,
	sdk KongCredentialBasicAuthSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", cred, client.ObjectKeyFromObject(cred))
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
		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cred.Status.Konnect.SetKonnectID(*resp.BasicAuth.ID)
	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// updateKongCredentialBasicAuth updates the Konnect BasicAuth entity.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the BasicAuth does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialBasicAuth(
	ctx context.Context,
	sdk KongCredentialBasicAuthSDK,
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect ControlPlane ID", cred, client.ObjectKeyFromObject(cred))
	}

	_, err := sdk.UpsertBasicAuthWithConsumer(ctx, sdkkonnectops.UpsertBasicAuthWithConsumerRequest{
		ControlPlaneID:              cpID,
		ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
		BasicAuthID:                 cred.GetKonnectStatus().GetKonnectID(),
		BasicAuthWithoutParents:     kongCredentialBasicAuthToBasicAuthWithoutParents(cred),
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

// deleteKongCredentialBasicAuth deletes a BasicAuth credential in Konnect.
// It is assumed that the provided BasicAuth has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialBasicAuth(
	ctx context.Context,
	sdk KongCredentialBasicAuthSDK,
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
			return FailedKonnectOpError[configurationv1alpha1.KongCredentialBasicAuth]{
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

func kongCredentialBasicAuthToBasicAuthWithoutParents(
	cred *configurationv1alpha1.KongCredentialBasicAuth,
) sdkkonnectcomp.BasicAuthWithoutParents {
	var (
		specTags       = cred.Spec.Tags
		annotationTags = metadata.ExtractTags(cred)
		k8sTags        = GenerateKubernetesMetadataTags(cred)
	)
	// Deduplicate tags to avoid rejection by Konnect.
	tags := lo.Uniq(slices.Concat(specTags, annotationTags, k8sTags))

	return sdkkonnectcomp.BasicAuthWithoutParents{
		Password: lo.ToPtr(cred.Spec.Password),
		Username: lo.ToPtr(cred.Spec.Username),
		Tags:     tags,
	}
}
