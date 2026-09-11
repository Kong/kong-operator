package ops

import (
	"context"
	"encoding/json"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// resolveVaultTarget resolves the KongVault named vaultName to the Konnect
// control plane ID and Config Store ID needed to push a secret into its
// backing Config Store, along with the vault's prefix.
func resolveVaultTarget(ctx context.Context, cl client.Client, vaultName string) (cpID, storeID, prefix string, err error) {
	var vault configurationv1alpha1.KongVault
	if err := cl.Get(ctx, client.ObjectKey{Name: vaultName}, &vault); err != nil {
		return "", "", "", fmt.Errorf("failed to get KongVault %q referenced by %s: %w", vaultName, consts.VaultSecretAnnotation, err)
	}

	cpID = vault.GetControlPlaneID()
	if cpID == "" {
		return "", "", "", fmt.Errorf("KongVault %q is not programmed on Konnect yet", vaultName)
	}

	storeID, err = vault.ResolveConfigStoreID(ctx, cl)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to resolve Config Store for KongVault %q: %w", vaultName, err)
	}
	if storeID == "" {
		storeID, err = configStoreIDFromRawConfig(vault.Spec.Config.Raw)
		if err != nil {
			return "", "", "", fmt.Errorf("KongVault %q: %w", vaultName, err)
		}
	}
	if storeID == "" {
		return "", "", "", fmt.Errorf("KongVault %q has no Config Store configured (spec.configStoreRef or spec.config.config_store_id)", vaultName)
	}

	return cpID, storeID, vault.Spec.Prefix, nil
}

// configStoreIDFromRawConfig reads config_store_id out of a KongVault's
// spec.config, for vaults that set it directly instead of via configStoreRef.
func configStoreIDFromRawConfig(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", fmt.Errorf("spec.config is not a valid JSON object: %w", err)
	}
	id, _ := config[configurationv1alpha1.KongVaultConfigStoreIDKey].(string)
	return id, nil
}

// vaultSecretKeyName derives a stable Config Store secret key name for the
// tls.key value of the given Secret. It is deterministic so that rotations
// overwrite the same Config Store entry instead of creating new ones.
//
// The namespace and name are each length-prefixed so that the boundary
// between them is unambiguous regardless of the hyphens they may themselves
// contain: without this, namespace "foo-bar"/name "baz" and namespace
// "foo"/name "bar-baz" would otherwise both produce "foo-bar-baz-tls-key" and
// collide on the same Config Store entry.
func vaultSecretKeyName(secret *corev1.Secret) string {
	return fmt.Sprintf("k8s-%d-%s-%d-%s-tls-key", len(secret.Namespace), secret.Namespace, len(secret.Name), secret.Name)
}

// pushKeyToVault ensures keyValue (the Secret's tls.key) is stored under a
// stable key in the Config Store backing the KongVault named by the Secret's
// consts.VaultSecretAnnotation, and returns the vault:// reference to use in
// its place, along with the status to record on the owning KongCertificate so
// that deletion can remove the same entry later even if the Secret is gone by
// then. marked is false, with no error, when the Secret does not carry the
// annotation, in which case keyValue should be sent to Konnect as-is.
//
// Config Store secrets carry no ownership metadata, so an entry that already
// exists at the deterministic key name computed above is adopted rather than
// rejected: this is indistinguishable from a prior push whose Konnect write
// succeeded but whose status update did not (e.g. the controller restarted in
// between), and refusing to proceed would turn that ordinary case into a
// stuck reconcile.
func pushKeyToVault(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.ConfigStoreSecretsSDK,
	secret *corev1.Secret,
	keyValue string,
) (vaultRef string, status *configurationv1alpha1.KongCertificateVaultSecretStatus, marked bool, err error) {
	vaultName, marked := secret.Annotations[consts.VaultSecretAnnotation]
	if !marked || vaultName == "" {
		return "", nil, false, nil
	}

	cpID, storeID, prefix, err := resolveVaultTarget(ctx, cl, vaultName)
	if err != nil {
		return "", nil, true, err
	}

	keyName := vaultSecretKeyName(secret)

	_, getErr := sdk.GetConfigStoreSecret(ctx, sdkkonnectops.GetConfigStoreSecretRequest{
		ControlPlaneID: cpID,
		ConfigStoreID:  storeID,
		Key:            keyName,
	})
	switch {
	case getErr == nil:
		_, err = sdk.UpdateConfigStoreSecret(ctx, sdkkonnectops.UpdateConfigStoreSecretRequest{
			ControlPlaneID: cpID,
			ConfigStoreID:  storeID,
			Key:            keyName,
			UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
				Value: keyValue,
			},
		})
	case ErrIsNotFound(getErr):
		_, err = sdk.CreateConfigStoreSecret(ctx, sdkkonnectops.CreateConfigStoreSecretRequest{
			ControlPlaneID: cpID,
			ConfigStoreID:  storeID,
			CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
				Key:   keyName,
				Value: keyValue,
			},
		})
	default:
		err = fmt.Errorf("failed to check for an existing Config Store secret %q: %w", keyName, getErr)
	}
	if err != nil {
		return "", nil, true, fmt.Errorf("failed to push Secret %s/%s's tls.key to KongVault %q's Config Store: %w",
			secret.Namespace, secret.Name, vaultName, err)
	}

	status = &configurationv1alpha1.KongCertificateVaultSecretStatus{
		ControlPlaneID: cpID,
		ConfigStoreID:  storeID,
		Key:            keyName,
	}
	return fmt.Sprintf("{vault://%s/%s}", prefix, keyName), status, true, nil
}

// deleteVaultSecret removes the Config Store secret identified by vs. It is a
// no-op, not an error, if the secret is already gone.
func deleteVaultSecret(ctx context.Context, sdk sdkkonnectgo.ConfigStoreSecretsSDK, vs *configurationv1alpha1.KongCertificateVaultSecretStatus) error {
	_, err := sdk.DeleteConfigStoreSecret(ctx, sdkkonnectops.DeleteConfigStoreSecretRequest{
		ControlPlaneID: vs.ControlPlaneID,
		ConfigStoreID:  vs.ConfigStoreID,
		Key:            vs.Key,
	})
	if err != nil && !ErrIsNotFound(err) {
		return err
	}
	return nil
}
