package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createVault(ctx context.Context, sdk sdkops.VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: vault, Op: CreateOp}
	}

	vaultInput, err := kongVaultToVaultInput(vault)
	if err != nil {
		return fmt.Errorf("failed to convert KongVault to Konnect vault input: %w", err)
	}
	resp, err := sdk.CreateVault(ctx, cpID, vaultInput)

	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, vault); errWrapped != nil {
		return errWrapped
	}

	if resp == nil || resp.Vault == nil || resp.Vault.ID == nil || *resp.Vault.ID == "" {
		return fmt.Errorf("failed creating %s: %w", vault.GetTypeName(), ErrNilResponse)
	}

	vault.SetKonnectID(*resp.Vault.ID)
	return nil
}

func updateVault(ctx context.Context, sdk sdkops.VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: vault, Op: UpdateOp}
	}

	id := vault.GetKonnectID()
	vaultInput, err := kongVaultToVaultInput(vault)
	if err != nil {
		return fmt.Errorf("failed to convert KongVault to Konnect vault input: %w", err)
	}

	_, err = sdk.UpsertVault(ctx, sdkkonnectops.UpsertVaultRequest{
		VaultID:        id,
		ControlPlaneID: cpID,
		Vault:          vaultInput,
	})

	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, vault); errWrapped != nil {
		return errWrapped
	}

	return nil
}

func deleteVault(ctx context.Context, sdk sdkops.VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: vault, Op: DeleteOp}
	}

	id := vault.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteVault(ctx, cpID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, vault); errWrapped != nil {
		return handleDeleteError(ctx, err, vault)
	}

	return nil
}

func adoptVault(ctx context.Context, sdk sdkops.VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return KonnectEntityAdoptionMissingControlPlaneIDError{}
	}

	adoptOptions := vault.Spec.Adopt
	if adoptOptions == nil || adoptOptions.Konnect == nil {
		return fmt.Errorf("failed to adopt: missing Konnect ID")
	}

	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetVault(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.Vault == nil {
		return fmt.Errorf("failed to adopt %s: %w", vault.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.Vault.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(vault.UID) {
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
		vaultCopy := vault.DeepCopy()
		vaultCopy.SetKonnectID(konnectID)
		if err = updateVault(ctx, sdk, vaultCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !vaultMatch(resp.Vault, vault) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	vault.SetKonnectID(konnectID)

	return nil
}

func kongVaultToVaultInput(vault *configurationv1alpha1.KongVault) (sdkkonnectcomp.Vault, error) {
	vaultConfig := map[string]any{}
	err := json.Unmarshal(vault.Spec.Config.Raw, &vaultConfig)
	if err != nil {
		return sdkkonnectcomp.Vault{}, err
	}
	input := sdkkonnectcomp.Vault{
		Config: vaultConfig,
		Name:   vault.Spec.Backend,
		Prefix: vault.Spec.Prefix,
		Tags:   GenerateTagsForObject(vault, vault.Spec.Tags...),
	}
	if vault.Spec.Description != "" {
		input.Description = lo.ToPtr(vault.Spec.Description)
	}
	return input, nil
}

func vaultMatch(konnectVault *sdkkonnectcomp.Vault, vault *configurationv1alpha1.KongVault) bool {
	if konnectVault == nil {
		return false
	}

	expected, err := kongVaultToVaultInput(vault)
	if err != nil {
		return false
	}

	return konnectVault.Name == expected.Name &&
		konnectVault.Prefix == expected.Prefix &&
		equalWithDefault(konnectVault.Description, expected.Description, "") &&
		vaultConfigMatch(konnectVault.Config, expected.Config)
}

func vaultConfigMatch(a map[string]any, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func getKongVaultForUID(
	ctx context.Context,
	sdk sdkops.VaultSDK,
	vault *configurationv1alpha1.KongVault,
) (string, error) {
	resp, err := sdk.ListVault(ctx, sdkkonnectops.ListVaultRequest{
		ControlPlaneID: vault.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(vault)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list KongVaults: %w", err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed to list KongVaults: %w", ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), vault)
}
