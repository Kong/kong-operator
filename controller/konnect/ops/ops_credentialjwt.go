package ops

import (
	"context"
	"errors"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createKongCredentialJWT(
	ctx context.Context,
	sdk KongCredentialJWTSDK,
	cred *configurationv1alpha1.KongCredentialJWT,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: CreateOp}
	}

	resp, err := sdk.CreateJwtWithConsumer(ctx,
		sdkkonnectops.CreateJwtWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			JWTWithoutParents:           kongCredentialJWTToJWTWithoutParents(cred),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cred); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cred, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cred.Status.Konnect.SetKonnectID(*resp.Jwt.ID)
	SetKonnectEntityProgrammedCondition(cred)

	return nil
}

// updateKongCredentialJWT updates the Konnect JWT entity.
// It is assumed that the provided JWT has Konnect ID set in status.
// It returns an error if the JWT does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialJWT(
	ctx context.Context,
	sdk KongCredentialJWTSDK,
	cred *configurationv1alpha1.KongCredentialJWT,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: UpdateOp}
	}

	_, err := sdk.UpsertJwtWithConsumer(ctx,
		sdkkonnectops.UpsertJwtWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			JWTID:                       cred.GetKonnectStatus().GetKonnectID(),
			JWTWithoutParents:           kongCredentialJWTToJWTWithoutParents(cred),
		})

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		// JWT update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				if err := createKongCredentialJWT(ctx, sdk, cred); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongCredentialJWT]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongCredentialJWT]{
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

// deleteKongCredentialJWT deletes an JWT credential in Konnect.
// It is assumed that the provided JWT has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialJWT(
	ctx context.Context,
	sdk KongCredentialJWTSDK,
	cred *configurationv1alpha1.KongCredentialJWT,
) error {
	cpID := cred.GetControlPlaneID()
	id := cred.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteJwtWithConsumer(ctx,
		sdkkonnectops.DeleteJwtWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			JWTID:                       id,
		})
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cred); errWrap != nil {
		return handleDeleteError(ctx, err, cred)
	}

	return nil
}

func kongCredentialJWTToJWTWithoutParents(
	cred *configurationv1alpha1.KongCredentialJWT,
) sdkkonnectcomp.JWTWithoutParents {
	ret := sdkkonnectcomp.JWTWithoutParents{
		Key:          cred.Spec.Key,
		Algorithm:    (*sdkkonnectcomp.JWTWithoutParentsAlgorithm)(&cred.Spec.Algorithm),
		RsaPublicKey: cred.Spec.RSAPublicKey,
		Secret:       cred.Spec.Secret,
		Tags:         GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
	return ret
}
