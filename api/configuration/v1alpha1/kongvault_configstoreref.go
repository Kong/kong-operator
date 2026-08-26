package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// KongVaultBackendKonnect is the name of the Konnect vault backend, the only
	// backend for which spec.configStoreRef is supported.
	KongVaultBackendKonnect = "konnect"

	// KongVaultConfigStoreIDKey is the key under spec.config that holds the Konnect
	// Config Store ID of a vault using the Konnect backend.
	KongVaultConfigStoreIDKey = "config_store_id"
)

// ConfigStoreRefInvalidError is returned when spec.configStoreRef cannot be used,
// regardless of the state of the referenced object. Waiting does not resolve it:
// the KongVault spec has to change.
//
// +kubebuilder:object:generate=false
type ConfigStoreRefInvalidError struct {
	Reason string
}

// Error implements the error interface.
func (e ConfigStoreRefInvalidError) Error() string {
	return fmt.Sprintf("spec.configStoreRef cannot be used: %s", e.Reason)
}

// GetConfigStoreRefNamespacedName returns the namespaced name of the
// KonnectConfigStore referenced by spec.configStoreRef, and whether the
// reference is set at all.
func (v *KongVault) GetConfigStoreRefNamespacedName() (client.ObjectKey, bool) {
	if v.Spec.ConfigStoreRef == nil {
		return client.ObjectKey{}, false
	}
	return client.ObjectKey{
		Namespace: v.Spec.ConfigStoreRef.Namespace,
		Name:      v.Spec.ConfigStoreRef.Name,
	}, true
}

// ResolveConfigStoreID resolves spec.configStoreRef to the Konnect ID of the
// referenced KonnectConfigStore, to be used as the `config_store_id` of the
// Konnect vault backend.
//
// It returns an empty ID and no error when spec.configStoreRef is unset, which
// leaves any `config_store_id` set directly in spec.config untouched.
//
// The returned error is one of:
//   - ConfigStoreRefInvalidError when the reference is not allowed for this KongVault.
//   - ReferenceNotFoundError when the KonnectConfigStore does not exist.
//   - ReferenceNotProgrammedError when the KonnectConfigStore exists
//     but has not been programmed in Konnect yet, so its ID is not known.
func (v *KongVault) ResolveConfigStoreID(ctx context.Context, cl client.Client) (string, error) {
	ref := v.Spec.ConfigStoreRef
	if ref == nil {
		return "", nil
	}

	// Enforced by CEL as well, but the reconciler must not rely on validation
	// alone: CRDs can be applied by an older operator version, and objects
	// created before the rule existed are not revalidated until updated.
	if v.Spec.Backend != KongVaultBackendKonnect {
		return "", ConfigStoreRefInvalidError{
			Reason: fmt.Sprintf("it is only supported with the %q backend, but spec.backend is %q",
				KongVaultBackendKonnect, v.Spec.Backend),
		}
	}

	// spec.config is a free-form JSON object, so this conflict cannot be expressed
	// as a CEL rule: fields under x-kubernetes-preserve-unknown-fields are not
	// visible to CEL.
	hasID, err := v.configHasConfigStoreID()
	if err != nil {
		return "", ConfigStoreRefInvalidError{
			Reason: fmt.Sprintf("spec.config is not a valid JSON object: %v", err),
		}
	}
	if hasID {
		return "", ConfigStoreRefInvalidError{
			Reason: fmt.Sprintf("it is mutually exclusive with %q in spec.config", KongVaultConfigStoreIDKey),
		}
	}

	nn, _ := v.GetConfigStoreRefNamespacedName()
	var configStore konnectv1alpha1.KonnectConfigStore
	if err := cl.Get(ctx, nn, &configStore); err != nil {
		if apierrors.IsNotFound(err) {
			return "", ReferenceNotFoundError{
				Kind:      KonnectConfigStoreKind,
				Namespace: nn.Namespace,
				Name:      nn.Name,
				Err:       err,
			}
		}
		return "", fmt.Errorf("failed to get referenced %s %s: %w", KonnectConfigStoreKind, nn, err)
	}

	// An empty Konnect ID means the Config Store has not been created in Konnect
	// yet. Waiting for it is expected, so this is reported as not-programmed
	// rather than as an invalid reference.
	if id := configStore.GetKonnectID(); id != "" {
		return id, nil
	}

	return "", ReferenceNotProgrammedError{
		Kind:      KonnectConfigStoreKind,
		Namespace: nn.Namespace,
		Name:      nn.Name,
	}
}

// configHasConfigStoreID reports whether spec.config sets config_store_id directly.
func (v *KongVault) configHasConfigStoreID() (bool, error) {
	if len(v.Spec.Config.Raw) == 0 {
		return false, nil
	}
	config := map[string]any{}
	if err := json.Unmarshal(v.Spec.Config.Raw, &config); err != nil {
		return false, err
	}
	_, ok := config[KongVaultConfigStoreIDKey]
	return ok, nil
}
