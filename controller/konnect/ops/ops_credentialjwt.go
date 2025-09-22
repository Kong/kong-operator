package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createKongCredentialJWT(
	ctx context.Context,
	sdk sdkops.KongCredentialJWTSDK,
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
		return errWrap
	}

	if resp == nil || resp.Jwt == nil || resp.Jwt.ID == nil {
		return fmt.Errorf("failed creating %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	cred.SetKonnectID(*resp.Jwt.ID)

	return nil
}

// updateKongCredentialJWT updates the Konnect JWT entity.
// It is assumed that the provided JWT has Konnect ID set in status.
// It returns an error if the JWT does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialJWT(
	ctx context.Context,
	sdk sdkops.KongCredentialJWTSDK,
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

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKongCredentialJWT deletes an JWT credential in Konnect.
// It is assumed that the provided JWT has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialJWT(
	ctx context.Context,
	sdk sdkops.KongCredentialJWTSDK,
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
) *sdkkonnectcomp.JWTWithoutParents {
	ret := &sdkkonnectcomp.JWTWithoutParents{
		Key:          cred.Spec.Key,
		Algorithm:    (*sdkkonnectcomp.JWTWithoutParentsAlgorithm)(&cred.Spec.Algorithm),
		RsaPublicKey: cred.Spec.RSAPublicKey,
		Secret:       cred.Spec.Secret,
		Tags:         GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
	return ret
}

// getKongCredentialJWTForUID lists JWT credentials in Konnect with given k8s uid as its tag.
func getKongCredentialJWTForUID(
	ctx context.Context,
	sdk sdkops.KongCredentialJWTSDK,
	cred *configurationv1alpha1.KongCredentialJWT,
) (string, error) {
	cpID := cred.GetControlPlaneID()

	req := sdkkonnectops.ListJwtRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(cred)),
	}

	resp, err := sdk.ListJwt(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cred)
}
