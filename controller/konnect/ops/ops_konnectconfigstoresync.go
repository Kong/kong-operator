package ops

import (
	"context"
	"fmt"
	"net/http"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// UpsertConfigStoreSecret writes one Config Store entry with PUT. Konnect
// returns 201 when the key was created and 200 when it was updated.
func UpsertConfigStoreSecret(
	ctx context.Context,
	sdk sdkkonnectgo.ConfigStoreSecretsSDK,
	controlPlaneID, configStoreID, key, value string,
) (created bool, updatedAt time.Time, err error) {
	updateResp, err := sdk.UpdateConfigStoreSecret(ctx, sdkkonnectops.UpdateConfigStoreSecretRequest{
		ControlPlaneID: controlPlaneID,
		ConfigStoreID:  configStoreID,
		Key:            key,
		UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
			Value: value,
		},
	})
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to upsert config store secret: %w", err)
	}
	if updateResp == nil || updateResp.ConfigStoreSecret == nil {
		return false, time.Time{}, ErrNilResponse
	}
	return updateResp.StatusCode == http.StatusCreated, derefTime(updateResp.ConfigStoreSecret.UpdatedAt), nil
}

// GetConfigStoreSecretMetadata returns the store-side updated_at timestamp of
// one entry. Reads are metadata-only: the value is never returned by the API.
// found is false when the entry does not exist.
func GetConfigStoreSecretMetadata(
	ctx context.Context,
	sdk sdkkonnectgo.ConfigStoreSecretsSDK,
	controlPlaneID, configStoreID, key string,
) (updatedAt time.Time, found bool, err error) {
	resp, err := sdk.GetConfigStoreSecret(ctx, sdkkonnectops.GetConfigStoreSecretRequest{
		ControlPlaneID: controlPlaneID,
		ConfigStoreID:  configStoreID,
		Key:            key,
	})
	if err != nil {
		if ErrIsNotFound(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("failed to get config store secret: %w", err)
	}
	if resp == nil || resp.ConfigStoreSecret == nil {
		return time.Time{}, false, ErrNilResponse
	}
	return derefTime(resp.ConfigStoreSecret.UpdatedAt), true, nil
}

// DeleteConfigStoreSecret deletes one entry. A missing entry (404) is
// treated as already deleted.
func DeleteConfigStoreSecret(
	ctx context.Context,
	sdk sdkkonnectgo.ConfigStoreSecretsSDK,
	controlPlaneID, configStoreID, key string,
) error {
	_, err := sdk.DeleteConfigStoreSecret(ctx, sdkkonnectops.DeleteConfigStoreSecretRequest{
		ControlPlaneID: controlPlaneID,
		ConfigStoreID:  configStoreID,
		Key:            key,
	})
	if err != nil {
		if ErrIsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete config store secret: %w", err)
	}
	return nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
