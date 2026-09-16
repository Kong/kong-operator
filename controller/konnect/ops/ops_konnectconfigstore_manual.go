package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// configStoreNotEmptyMessage is the (case-insensitive) substring of the error
// detail that Konnect returns when rejecting the deletion of a config store
// that still holds secret entries. The Konnect API exposes no machine-readable
// error code for this case, so detection relies on the typed 400 error plus
// this message, centralized here.
const configStoreNotEmptyMessage = "can not delete config store with secrets"

const (
	// configStoreSecretsListPageSize is the page size used when listing the
	// secret entries that block a config store deletion.
	configStoreSecretsListPageSize = 100
	// configStoreSecretsListMaxPages bounds the listing of blocking entries so
	// a huge store cannot make a single reconcile excessively long. The listed
	// keys are only used for the user-facing condition message; the deletion
	// remains blocked regardless.
	configStoreSecretsListMaxPages = 10
)

// KonnectConfigStoreNotEmptyError is returned when Konnect rejects the deletion
// of a KonnectConfigStore because the config store still holds secret entries.
// Deletion is blocked until the user removes the entries from the store.
type KonnectConfigStoreNotEmptyError struct {
	// ConfigStoreID is the Konnect ID of the config store.
	ConfigStoreID string
	// Keys are the keys of the secret entries blocking the deletion, sorted
	// alphabetically. Nil when listing the entries failed.
	Keys []string
	// Err is the underlying Konnect API error.
	Err error
}

// Error implements the error interface.
func (e KonnectConfigStoreNotEmptyError) Error() string {
	if e.Keys == nil {
		return fmt.Sprintf(
			"config store %s still holds secret entries (listing them failed), deletion blocked: %v",
			e.ConfigStoreID, e.Err,
		)
	}
	return fmt.Sprintf(
		"config store %s still holds %d secret entries, deletion blocked: %v",
		e.ConfigStoreID, len(e.Keys), e.Err,
	)
}

// Unwrap returns the underlying Konnect API error.
func (e KonnectConfigStoreNotEmptyError) Unwrap() error {
	return e.Err
}

// maxKeysInDeletionBlockedMessage caps the number of entry keys included in
// the user-facing DeletionBlocked condition message.
const maxKeysInDeletionBlockedMessage = 10

// DeletionBlockedMessage returns a user-facing message describing that the
// config store deletion is blocked by the secret entries it still holds, so
// the user knows exactly what to clean up before deletion can proceed.
func (e KonnectConfigStoreNotEmptyError) DeletionBlockedMessage() string {
	if e.Keys == nil {
		return "deletion blocked: the config store still holds secret entries; " +
			"remove the entries from Konnect and the deletion will proceed automatically"
	}
	if len(e.Keys) == 0 {
		return "deletion blocked: the config store still holds secret entries " +
			"(could not list their keys); remove the entries from Konnect and the deletion will proceed automatically"
	}
	if len(e.Keys) <= maxKeysInDeletionBlockedMessage {
		return fmt.Sprintf(
			"deletion blocked: the config store still holds %d secret entries; "+
				"remove the entries with keys %v and the deletion will proceed automatically",
			len(e.Keys), e.Keys,
		)
	}
	return fmt.Sprintf(
		"deletion blocked: the config store still holds %d secret entries; "+
			"remove the entries (first %d keys: %v) and the deletion will proceed automatically",
		len(e.Keys), maxKeysInDeletionBlockedMessage, e.Keys[:maxKeysInDeletionBlockedMessage],
	)
}

// ErrorIsConfigStoreNotEmpty returns true if the provided error is a Konnect
// 400 rejecting the deletion of a config store that still holds secret entries.
func ErrorIsConfigStoreNotEmpty(err error) bool {
	if errBadRequest, ok := errors.AsType[*sdkkonnecterrs.BadRequestError](err); ok {
		return strings.Contains(
			strings.ToLower(errBadRequest.Detail),
			configStoreNotEmptyMessage,
		)
	}
	if errSDK, ok := errors.AsType[*sdkkonnecterrs.SDKError](err); ok {
		return errSDK.StatusCode == http.StatusBadRequest &&
			strings.Contains(strings.ToLower(errSDK.Body), configStoreNotEmptyMessage)
	}
	return false
}

// deleteKonnectConfigStoreGuarded deletes a KonnectConfigStore, but when
// Konnect refuses the deletion because the store still holds secret entries,
// it lists the blocking entry keys and returns a KonnectConfigStoreNotEmptyError
// so the reconciler can report a DeletionBlocked condition instead of silently
// cascade-deleting the entries.
func deleteKonnectConfigStoreGuarded(
	ctx context.Context,
	configStoresSDK sdkkonnectgo.ConfigStoresSDK,
	configStoreSecretsSDK sdkkonnectgo.ConfigStoreSecretsSDK,
	obj *konnectv1alpha1.KonnectConfigStore,
) error {
	err := deleteKonnectConfigStore(ctx, configStoresSDK, obj)
	if err == nil || !ErrorIsConfigStoreNotEmpty(err) {
		return err
	}

	keys, listErr := listConfigStoreSecretKeys(ctx, configStoreSecretsSDK, obj)
	if listErr != nil {
		ctrllog.FromContext(ctx).
			Info("failed to list config store secret entries blocking deletion",
				"type", obj.GetTypeName(),
				"id", obj.GetKonnectStatus().GetKonnectID(),
				"error", listErr.Error(),
			)
	}
	return KonnectConfigStoreNotEmptyError{
		ConfigStoreID: obj.GetKonnectStatus().GetKonnectID(),
		Keys:          keys,
		Err:           err,
	}
}

// listConfigStoreSecretKeys lists the keys of all secret entries held by the
// Konnect config store backing the provided KonnectConfigStore, sorted
// alphabetically.
func listConfigStoreSecretKeys(
	ctx context.Context,
	configStoreSecretsSDK sdkkonnectgo.ConfigStoreSecretsSDK,
	obj *konnectv1alpha1.KonnectConfigStore,
) ([]string, error) {
	var (
		keys      []string
		pageAfter *string
	)
	for range configStoreSecretsListMaxPages {
		resp, err := configStoreSecretsSDK.ListConfigStoreSecrets(ctx, sdkkonnectops.ListConfigStoreSecretsRequest{
			ControlPlaneID: obj.GetControlPlaneID(),
			ConfigStoreID:  obj.GetKonnectStatus().GetKonnectID(),
			PageSize:       new(int64(configStoreSecretsListPageSize)),
			PageAfter:      pageAfter,
		})
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.ListConfigStoreSecretsResponse == nil {
			return nil, ErrNilResponse
		}

		for _, secret := range resp.ListConfigStoreSecretsResponse.Data {
			if key := secret.GetKey(); key != nil {
				keys = append(keys, *key)
			}
		}

		next := resp.ListConfigStoreSecretsResponse.Meta.Page.GetNext()
		if next == nil {
			break
		}
		pageAfter = next
	}

	slices.Sort(keys)
	return keys, nil
}
