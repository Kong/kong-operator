package index

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestKonnectConfigStoreSyncStoreKeys(t *testing.T) {
	sync := func() *konnectv1alpha1.KonnectConfigStoreSync {
		return &konnectv1alpha1.KonnectConfigStoreSync{
			Namespace: "default", Name: "my-sync",
			Spec: konnectv1alpha1.KonnectConfigStoreSyncSpec{
				ConfigStoreRef: commonv1alpha1.NamespacedRef{Name: "store"},
				SecretRef:      commonv1alpha1.NamespacedRef{Name: "secret"},
				Mode:           konnectv1alpha1.KonnectConfigStoreSyncModeCombined,
				Combined:       &konnectv1alpha1.KonnectConfigStoreSyncCombined{},
			},
		}
	}

	t.Run("no store ID in status -> not indexed", func(t *testing.T) {
		assert.Nil(t, konnectConfigStoreSyncStoreKeys(sync()))
	})

	t.Run("no durably owned entries in status -> not indexed", func(t *testing.T) {
		s := sync()
		s.Status.StoreID = "store-123"
		assert.Nil(t, konnectConfigStoreSyncStoreKeys(s))
	})

	t.Run("combined derived key", func(t *testing.T) {
		s := sync()
		s.Status.StoreID = "store-123"
		s.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			{StoreKey: "k8s-7-default-7-my-sync"},
		}
		assert.Equal(t,
			[]string{"store-123/k8s-7-default-7-my-sync"},
			konnectConfigStoreSyncStoreKeys(s),
		)
	})

	t.Run("combined explicit key", func(t *testing.T) {
		s := sync()
		s.Status.StoreID = "store-123"
		s.Spec.Combined.StoreKey = new("explicit-key")
		s.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			{StoreKey: "explicit-key"},
		}
		assert.Equal(t,
			[]string{"store-123/explicit-key"},
			konnectConfigStoreSyncStoreKeys(s),
		)
	})

	t.Run("split derived and explicit keys", func(t *testing.T) {
		s := sync()
		s.Status.StoreID = "store-123"
		s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
		s.Spec.Combined = nil
		s.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
			Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				{Field: "tls.crt"},
				{Field: "tls.key", StoreKey: new("custom-key")},
			},
		}
		s.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			{StoreKey: "k8s-7-default-7-my-sync-tls.crt"},
			{StoreKey: "custom-key"},
		}
		assert.Equal(t,
			[]string{
				"store-123/k8s-7-default-7-my-sync-tls.crt",
				"store-123/custom-key",
			},
			konnectConfigStoreSyncStoreKeys(s),
		)
	})

	t.Run("status-only and spec-only keys are not indexed", func(t *testing.T) {
		s := sync()
		s.Status.StoreID = "store-123"
		s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
		s.Spec.Combined = nil
		s.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
			Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				{Field: "current", StoreKey: new("current-key")},
				{Field: "never-written", StoreKey: new("spec-only-key")},
			},
		}
		s.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			{StoreKey: "current-key"},
			{StoreKey: "pending-cleanup-key"},
		}
		assert.Equal(t,
			[]string{"store-123/current-key"},
			konnectConfigStoreSyncStoreKeys(s),
		)
	})

	t.Run("wrong type -> nil", func(t *testing.T) {
		assert.Nil(t, konnectConfigStoreSyncStoreKeys(&konnectv1alpha1.KonnectConfigStore{}))
	})
}

func TestKonnectConfigStoreSyncRefIndexes(t *testing.T) {
	s := &konnectv1alpha1.KonnectConfigStoreSync{
		Namespace: "default", Name: "my-sync",
		Spec: konnectv1alpha1.KonnectConfigStoreSyncSpec{
			ConfigStoreRef: commonv1alpha1.NamespacedRef{Name: "store"},
			SecretRef:      commonv1alpha1.NamespacedRef{Name: "secret"},
		},
	}

	t.Run("namespaces default to the sync's", func(t *testing.T) {
		assert.Equal(t, []string{"default/store"}, konnectConfigStoreSyncConfigStoreRef(s))
		assert.Equal(t, []string{"default/secret"}, konnectConfigStoreSyncSecretRef(s))
	})

	t.Run("explicit namespaces are used", func(t *testing.T) {
		s := s.DeepCopy()
		s.Spec.ConfigStoreRef.Namespace = new("stores")
		s.Spec.SecretRef.Namespace = new("secrets")
		assert.Equal(t, []string{"stores/store"}, konnectConfigStoreSyncConfigStoreRef(s))
		assert.Equal(t, []string{"secrets/secret"}, konnectConfigStoreSyncSecretRef(s))
	})

	t.Run("wrong type -> nil", func(t *testing.T) {
		other := &konnectv1alpha1.KonnectConfigStore{}
		assert.Nil(t, konnectConfigStoreSyncConfigStoreRef(other))
		assert.Nil(t, konnectConfigStoreSyncSecretRef(other))
	})
}
