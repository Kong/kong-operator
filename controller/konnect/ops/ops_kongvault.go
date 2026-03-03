package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

type resolvedConfigStoreIDContextKey struct{}

// WithResolvedConfigStoreID makes a Config Store ID resolved during reference
// handling available to the KongVault operation in the same reconciliation.
func WithResolvedConfigStoreID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, resolvedConfigStoreIDContextKey{}, id)
}

func resolvedConfigStoreIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(resolvedConfigStoreIDContextKey{}).(string)
	return id, ok
}

func createVault(ctx context.Context, cl client.Client, sdk sdkkonnectgo.VaultsSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: vault, Op: CreateOp}
	}

	vaultInput, err := kongVaultToVaultInput(ctx, cl, vault)
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

func updateVault(ctx context.Context, cl client.Client, sdk sdkkonnectgo.VaultsSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: vault, Op: UpdateOp}
	}

	id := vault.GetKonnectID()
	vaultInput, err := kongVaultToVaultInput(ctx, cl, vault)
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

func deleteVault(ctx context.Context, sdk sdkkonnectgo.VaultsSDK, vault *configurationv1alpha1.KongVault) error {
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

func adoptVault(ctx context.Context, cl client.Client, sdk sdkkonnectgo.VaultsSDK, vault *configurationv1alpha1.KongVault) error {
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
		if err = updateVault(ctx, cl, sdk, vaultCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		matches, err := vaultMatch(ctx, cl, resp.Vault, vault)
		if err != nil {
			return err
		}
		if !matches {
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

// kongVaultToVaultInput converts a KongVault to the Konnect SDK vault input.
//
// When spec.configStoreRef is set, the Konnect ID of the referenced
// KonnectConfigStore is resolved through cl and injected into the vault
// configuration as config_store_id, so that users don't have to copy the
// Konnect-generated ID into spec.config themselves.
func kongVaultToVaultInput(
	ctx context.Context,
	cl client.Client,
	vault *configurationv1alpha1.KongVault,
) (sdkkonnectcomp.Vault, error) {
	vaultConfig := map[string]any{}
	// spec.config is optional: a Konnect backed vault can be fully configured
	// through spec.configStoreRef alone.
	if len(vault.Spec.Config.Raw) > 0 {
		if err := json.Unmarshal(vault.Spec.Config.Raw, &vaultConfig); err != nil {
			return sdkkonnectcomp.Vault{}, err
		}
	}
	configStoreID, resolved := resolvedConfigStoreIDFromContext(ctx)
	if !resolved {
		var err error
		configStoreID, err = vault.ResolveConfigStoreID(ctx, cl)
		if err != nil {
			return sdkkonnectcomp.Vault{}, fmt.Errorf("failed to resolve spec.configStoreRef: %w", err)
		}
	}
	if configStoreID != "" {
		vaultConfig[configurationv1alpha1.KongVaultConfigStoreIDKey] = configStoreID
	}
	input := sdkkonnectcomp.Vault{
		Config: vaultConfig,
		Name:   vault.Spec.Backend,
		Prefix: vault.Spec.Prefix,
		Tags:   GenerateTagsForObject(vault, vault.Spec.Tags...),
	}
	if vault.Spec.Description != "" {
		input.Description = new(vault.Spec.Description)
	}
	return input, nil
}

func vaultMatch(
	ctx context.Context,
	cl client.Client,
	konnectVault *sdkkonnectcomp.Vault,
	vault *configurationv1alpha1.KongVault,
) (bool, error) {
	if konnectVault == nil {
		return false, nil
	}

	// The error is returned rather than reported as a mismatch: a KongVault whose
	// spec cannot be converted is not "different from Konnect", it is unusable,
	// and reporting it as a mismatch would hide the actual cause.
	expected, err := kongVaultToVaultInput(ctx, cl, vault)
	if err != nil {
		return false, err
	}

	return konnectVault.Name == expected.Name &&
		konnectVault.Prefix == expected.Prefix &&
		equalWithDefault(konnectVault.Description, expected.Description, "") &&
		vaultConfigMatch(konnectVault.Config, expected.Config), nil
}

func vaultConfigMatch(a map[string]any, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func getKongVaultForUID(
	ctx context.Context,
	sdk sdkkonnectgo.VaultsSDK,
	vault *configurationv1alpha1.KongVault,
) (string, error) {
	resp, err := sdk.ListVault(ctx, sdkkonnectops.ListVaultRequest{
		ControlPlaneID: vault.GetControlPlaneID(),
		Tags:           new(UIDLabelForObject(vault)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list KongVaults: %w", err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed to list KongVaults: %w", ErrNilResponse)
	}

	_, id, err := getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), vault)
	return id, err
}
