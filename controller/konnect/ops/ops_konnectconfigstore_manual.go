package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// configStoreSecretsProbePageSize limits the follow-up query used to confirm
// that a failed config store deletion is blocked by secret entries.
const configStoreSecretsProbePageSize = 1

// KonnectConfigStoreNotEmptyError is returned when Konnect rejects the deletion
// of a KonnectConfigStore because the config store still holds secret entries.
// Deletion is blocked until the user removes the entries from the store.
type KonnectConfigStoreNotEmptyError struct {
	// ConfigStoreID is the Konnect ID of the config store.
	ConfigStoreID string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e KonnectConfigStoreNotEmptyError) Error() string {
	return fmt.Sprintf(
		"config store %s still holds secret entries, deletion blocked: %v",
		e.ConfigStoreID, e.Err,
	)
}

// Unwrap returns the underlying Konnect API error.
func (e KonnectConfigStoreNotEmptyError) Unwrap() error {
	return e.Err
}

// DeletionBlockedMessage returns a user-facing message describing that the
// config store deletion is blocked by the secret entries it still holds.
func (e KonnectConfigStoreNotEmptyError) DeletionBlockedMessage() string {
	return "deletion blocked: the config store still holds secret entries; " +
		"remove the entries from Konnect and the deletion will proceed automatically"
}

func errorIsBadRequest(err error) bool {
	if ErrorIsSDKBadRequestError(err) {
		return true
	}
	if errSDK, ok := errors.AsType[*sdkkonnecterrs.SDKError](err); ok {
		return errSDK.StatusCode == http.StatusBadRequest
	}
	return false
}

// deleteKonnectConfigStoreGuarded deletes a KonnectConfigStore, but when
// Konnect refuses the deletion because the store still holds secret entries,
// it probes for an entry and returns a KonnectConfigStoreNotEmptyError so the
// reconciler can report a DeletionBlocked condition instead of silently
// cascade-deleting the entries. The probe avoids relying on human-readable
// error details from Konnect.
func deleteKonnectConfigStoreGuarded(
	ctx context.Context,
	configStoresSDK sdkkonnectgo.ConfigStoresSDK,
	configStoreSecretsSDK sdkkonnectgo.ConfigStoreSecretsSDK,
	obj *konnectv1alpha1.KonnectConfigStore,
) error {
	err := deleteKonnectConfigStore(ctx, configStoresSDK, obj)
	if err == nil || !errorIsBadRequest(err) {
		return err
	}

	hasSecretEntries, listErr := configStoreHasSecretEntries(ctx, configStoreSecretsSDK, obj)
	if listErr != nil {
		ctrllog.FromContext(ctx).
			Info("failed to determine whether config store secret entries blocked deletion",
				"type", obj.GetTypeName(),
				"id", obj.GetKonnectStatus().GetKonnectID(),
				"error", listErr.Error(),
			)
		return err
	}
	if !hasSecretEntries {
		return err
	}
	return KonnectConfigStoreNotEmptyError{
		ConfigStoreID: obj.GetKonnectStatus().GetKonnectID(),
		Err:           err,
	}
}

// configStoreHasSecretEntries reports whether the Konnect config store holds
// at least one secret entry. It intentionally requests only one entry: the
// result is used solely to distinguish a deletion blocked by a non-empty store
// from other bad requests.
func configStoreHasSecretEntries(
	ctx context.Context,
	configStoreSecretsSDK sdkkonnectgo.ConfigStoreSecretsSDK,
	obj *konnectv1alpha1.KonnectConfigStore,
) (bool, error) {
	resp, err := configStoreSecretsSDK.ListConfigStoreSecrets(ctx, sdkkonnectops.ListConfigStoreSecretsRequest{
		ControlPlaneID: obj.GetControlPlaneID(),
		ConfigStoreID:  obj.GetKonnectStatus().GetKonnectID(),
		PageSize:       new(int64(configStoreSecretsProbePageSize)),
	})
	if err != nil {
		return false, err
	}
	if resp == nil || resp.ListConfigStoreSecretsResponse == nil {
		return false, ErrNilResponse
	}
	return len(resp.ListConfigStoreSecretsResponse.Data) > 0, nil
}
