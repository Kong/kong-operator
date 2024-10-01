package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createVault(ctx context.Context, sdk VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf(
			"can't create %T %s without a Konnect ControlPlane ID",
			vault, client.ObjectKeyFromObject(vault),
		)
	}

	vaultInput, err := kongVaultToVaultInput(vault)
	if err != nil {
		return fmt.Errorf("failed to convert KongVault to Konnect vault input: %w", err)
	}
	resp, err := sdk.CreateVault(ctx, cpID, vaultInput)

	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, vault); errWrapped != nil {
		SetKonnectEntityProgrammedConditionFalse(vault, "FailedToCreate", errWrapped.Error())
		return errWrapped
	}

	vault.SetKonnectID(*resp.Vault.ID)
	SetKonnectEntityProgrammedCondition(vault)
	return nil
}

func updateVault(ctx context.Context, sdk VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf(
			"can't update %T %s without a Konnect ControlPlane ID",
			vault, client.ObjectKeyFromObject(vault),
		)
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
		// Service update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				if err := createVault(ctx, sdk, vault); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongVault]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				// Create succeeded, createVault sets the status so no need to do this here.
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongVault]{
					Op:  UpdateOp,
					Err: sdkError,
				}
			}
		}

		SetKonnectEntityProgrammedConditionFalse(vault, "FailedToUpdate", errWrapped.Error())
		return errWrapped
	}

	SetKonnectEntityProgrammedCondition(vault)
	return nil
}

func deleteVault(ctx context.Context, sdk VaultSDK, vault *configurationv1alpha1.KongVault) error {
	cpID := vault.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf(
			"can't delete %T %s without a Konnect ControlPlane ID",
			vault, client.ObjectKeyFromObject(vault),
		)
	}
	id := vault.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteVault(ctx, cpID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, vault); errWrapped != nil {
		// Vault delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", vault.GetTypeName(), "id", id,
					)
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongVault]{
					Op:  DeleteOp,
					Err: sdkError,
				}
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongVault]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongVaultToVaultInput(vault *configurationv1alpha1.KongVault) (sdkkonnectcomp.VaultInput, error) {
	vaultConfig := map[string]any{}
	err := json.Unmarshal(vault.Spec.Config.Raw, &vaultConfig)
	if err != nil {
		return sdkkonnectcomp.VaultInput{}, err
	}
	input := sdkkonnectcomp.VaultInput{
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
