package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createKongCredentialHMAC(
	ctx context.Context,
	sdk sdkops.KongCredentialHMACSDK,
	cred *configurationv1alpha1.KongCredentialHMAC,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: CreateOp}
	}

	resp, err := sdk.CreateHmacAuthWithConsumer(ctx,
		sdkkonnectops.CreateHmacAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			HMACAuthWithoutParents:      kongCredentialHMACToHMACWithoutParents(cred),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cred); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.HMACAuth == nil || resp.HMACAuth.ID == nil {
		return fmt.Errorf("failed creating %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	cred.SetKonnectID(*resp.HMACAuth.ID)

	return nil
}

// updateKongCredentialHMAC updates the Konnect HMAC entity.
// It is assumed that the provided HMAC has Konnect ID set in status.
// It returns an error if the HMAC does not have a ControlPlaneRef or
// if the operation fails.
func updateKongCredentialHMAC(
	ctx context.Context,
	sdk sdkops.KongCredentialHMACSDK,
	cred *configurationv1alpha1.KongCredentialHMAC,
) error {
	cpID := cred.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cred, Op: UpdateOp}
	}

	_, err := sdk.UpsertHmacAuthWithConsumer(ctx,
		sdkkonnectops.UpsertHmacAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			HMACAuthID:                  cred.GetKonnectStatus().GetKonnectID(),
			HMACAuthWithoutParents:      kongCredentialHMACToHMACWithoutParents(cred),
		})

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cred); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteKongCredentialHMAC deletes an HMAC credential in Konnect.
// It is assumed that the provided HMAC has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteKongCredentialHMAC(
	ctx context.Context,
	sdk sdkops.KongCredentialHMACSDK,
	cred *configurationv1alpha1.KongCredentialHMAC,
) error {
	cpID := cred.GetControlPlaneID()
	id := cred.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteHmacAuthWithConsumer(ctx,
		sdkkonnectops.DeleteHmacAuthWithConsumerRequest{
			ControlPlaneID:              cpID,
			ConsumerIDForNestedEntities: cred.Status.Konnect.GetConsumerID(),
			HMACAuthID:                  id,
		})
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cred); errWrap != nil {
		return handleDeleteError(ctx, err, cred)
	}

	return nil
}

func kongCredentialHMACToHMACWithoutParents(
	cred *configurationv1alpha1.KongCredentialHMAC,
) sdkkonnectcomp.HMACAuthWithoutParents {
	ret := sdkkonnectcomp.HMACAuthWithoutParents{
		Username: *cred.Spec.Username,
		Secret:   cred.Spec.Secret,
		Tags:     GenerateTagsForObject(cred, cred.Spec.Tags...),
	}
	return ret
}

// getKongCredentialHMACForUID lists HMAC credentials in Konnect with given k8s uid as its tag.
func getKongCredentialHMACForUID(
	ctx context.Context,
	sdk sdkops.KongCredentialHMACSDK,
	cred *configurationv1alpha1.KongCredentialHMAC,
) (string, error) {
	cpID := cred.GetControlPlaneID()

	req := sdkkonnectops.ListHmacAuthRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(cred)),
	}

	resp, err := sdk.ListHmacAuth(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cred.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cred)
}
