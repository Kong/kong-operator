package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/configstoresync"
)

const (
	// IndexFieldKonnectConfigStoreSyncOnStoreKey is the index field for
	// KonnectConfigStoreSync -> resolved (storeID, storeKey) pairs. It backs
	// the Kubernetes-side conflict index: two syncs resolving to the same
	// (storeID, storeKey) pair are indexed under the same value.
	IndexFieldKonnectConfigStoreSyncOnStoreKey = "konnectConfigStoreSyncStoreKey"
	// IndexFieldKonnectConfigStoreSyncOnConfigStore is the index field for
	// KonnectConfigStoreSync -> referenced KonnectConfigStore
	// ("<namespace>/<name>", namespace defaulted to the sync's).
	IndexFieldKonnectConfigStoreSyncOnConfigStore = "konnectConfigStoreSyncConfigStoreRef"
	// IndexFieldKonnectConfigStoreSyncOnSecret is the index field for
	// KonnectConfigStoreSync -> referenced Secret ("<namespace>/<name>",
	// namespace defaulted to the sync's).
	IndexFieldKonnectConfigStoreSyncOnSecret = "konnectConfigStoreSyncSecretRef" // #nosec G101
)

// OptionsForKonnectConfigStoreSync returns required Index options for the
// KonnectConfigStoreSync reconciler.
func OptionsForKonnectConfigStoreSync() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectConfigStoreSync{},
			Field:          IndexFieldKonnectConfigStoreSyncOnStoreKey,
			ExtractValueFn: konnectConfigStoreSyncStoreKeys,
		},
		{
			Object:         &konnectv1alpha1.KonnectConfigStoreSync{},
			Field:          IndexFieldKonnectConfigStoreSyncOnConfigStore,
			ExtractValueFn: konnectConfigStoreSyncConfigStoreRef,
		},
		{
			Object:         &konnectv1alpha1.KonnectConfigStoreSync{},
			Field:          IndexFieldKonnectConfigStoreSyncOnSecret,
			ExtractValueFn: konnectConfigStoreSyncSecretRef,
		},
	}
}

// konnectConfigStoreSyncStoreKeys extracts "<storeID>/<storeKey>" for every
// entry the sync has durably recorded as owned and still declares in spec.
// Store ID and entry status are persisted only after the referenced store is
// observed and a write succeeds. A fail-closed sync that never wrote a key
// therefore cannot block a healthy sync, while a sync that wrote successfully
// before becoming invalid retains ownership of its serving value.
func konnectConfigStoreSyncStoreKeys(obj client.Object) []string {
	sync, ok := obj.(*konnectv1alpha1.KonnectConfigStoreSync)
	if !ok {
		return nil
	}
	if sync.Status.StoreID == "" || len(sync.Status.Entries) == 0 {
		return nil
	}
	owned := make(map[string]struct{}, len(sync.Status.Entries))
	for _, entry := range sync.Status.Entries {
		owned[entry.StoreKey] = struct{}{}
	}
	entries := configstoresync.ResolveEntries(sync)
	values := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, ok := owned[e.StoreKey]; ok {
			values = append(values, sync.Status.StoreID+"/"+e.StoreKey)
		}
	}
	return values
}

func konnectConfigStoreSyncConfigStoreRef(obj client.Object) []string {
	sync, ok := obj.(*konnectv1alpha1.KonnectConfigStoreSync)
	if !ok {
		return nil
	}
	ns := sync.Spec.ConfigStoreRef.Namespace
	if ns == nil || *ns == "" {
		ns = new(sync.Namespace)
	}
	return []string{*ns + "/" + sync.Spec.ConfigStoreRef.Name}
}

func konnectConfigStoreSyncSecretRef(obj client.Object) []string {
	sync, ok := obj.(*konnectv1alpha1.KonnectConfigStoreSync)
	if !ok {
		return nil
	}
	ns := sync.Spec.SecretRef.Namespace
	if ns == nil || *ns == "" {
		ns = new(sync.Namespace)
	}
	return []string{*ns + "/" + sync.Spec.SecretRef.Name}
}
