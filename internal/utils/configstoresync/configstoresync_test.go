package configstoresync

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestDerivedKey(t *testing.T) {
	assert.Equal(t, "k8s-7-default-4-sync", DerivedKey("default", "sync"))

	// The length prefixes make the encoding unambiguous: pairs that would
	// collide under a naive "k8s-<ns>-<name>" format must not collide here.
	assert.NotEqual(t, DerivedKey("a-b", "c"), DerivedKey("a", "b-c"))
	assert.NotEqual(t, DerivedKey("ab", "c"), DerivedKey("a", "bc"))
}

func TestResolveEntries(t *testing.T) {
	newSync := func(mutate func(*konnectv1alpha1.KonnectConfigStoreSync)) *konnectv1alpha1.KonnectConfigStoreSync {
		sync := &konnectv1alpha1.KonnectConfigStoreSync{
			Namespace: "default", Name: "sync",
		}
		if mutate != nil {
			mutate(sync)
		}
		return sync
	}

	t.Run("combined defaults", func(t *testing.T) {
		entries := ResolveEntries(newSync(nil))
		require.Len(t, entries, 1)
		assert.Equal(t, DerivedKey("default", "sync"), entries[0].StoreKey)
		assert.Equal(t, []string{"tls.crt", "tls.key"}, entries[0].SourceFields)
		assert.Equal(t, []string{CombinedCertificateSubField, CombinedKeySubField}, entries[0].SubFields)
	})

	t.Run("combined explicit key and fields", func(t *testing.T) {
		entries := ResolveEntries(newSync(func(s *konnectv1alpha1.KonnectConfigStoreSync) {
			s.Spec.Combined = &konnectv1alpha1.KonnectConfigStoreSyncCombined{
				StoreKey:         new("my-key"),
				CertificateField: "cert.pem",
				KeyField:         "key.pem",
			}
		}))
		require.Len(t, entries, 1)
		assert.Equal(t, "my-key", entries[0].StoreKey)
		assert.Equal(t, []string{"cert.pem", "key.pem"}, entries[0].SourceFields)
	})

	t.Run("split derived and explicit keys", func(t *testing.T) {
		entries := ResolveEntries(newSync(func(s *konnectv1alpha1.KonnectConfigStoreSync) {
			s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
			s.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
				Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
					{Field: "tls.crt"},
					{Field: "tls.key", StoreKey: new("explicit-key")},
				},
			}
		}))
		require.Len(t, entries, 2)
		assert.Equal(t, DerivedKey("default", "sync")+"-tls.crt", entries[0].StoreKey)
		assert.Equal(t, []string{"tls.crt"}, entries[0].SourceFields)
		assert.Empty(t, entries[0].SubFields, "Split entries store raw values")
		assert.Equal(t, "explicit-key", entries[1].StoreKey)
	})

	t.Run("split without split spec resolves nothing", func(t *testing.T) {
		entries := ResolveEntries(newSync(func(s *konnectv1alpha1.KonnectConfigStoreSync) {
			s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
		}))
		assert.Empty(t, entries)
	})

	t.Run("explicit storeKey can collide with another entry's derived key", func(t *testing.T) {
		// Pins the gap CRD validation cannot express: explicit storeKeys are
		// only checked against each other, never against derived keys, so
		// ResolveEntries can return duplicate StoreKeys. Callers must reject
		// this (see DuplicateStoreKeys).
		entries := ResolveEntries(newSync(func(s *konnectv1alpha1.KonnectConfigStoreSync) {
			s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
			s.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{
				Entries: []konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
					{Field: "tls.crt"},
					{Field: "tls.key", StoreKey: new(DerivedKey("default", "sync") + "-tls.crt")},
				},
			}
		}))
		require.Len(t, entries, 2)
		assert.Equal(t, entries[0].StoreKey, entries[1].StoreKey)
		assert.Equal(t, []string{entries[0].StoreKey}, DuplicateStoreKeys(entries))
	})
}

func TestDuplicateStoreKeys(t *testing.T) {
	t.Run("distinct keys return nil", func(t *testing.T) {
		entries := []ResolvedEntry{{StoreKey: "a"}, {StoreKey: "b"}}
		assert.Nil(t, DuplicateStoreKeys(entries))
	})

	t.Run("every duplicate occurrence is reported", func(t *testing.T) {
		entries := []ResolvedEntry{{StoreKey: "a"}, {StoreKey: "b"}, {StoreKey: "a"}, {StoreKey: "a"}}
		assert.Equal(t, []string{"a", "a"}, DuplicateStoreKeys(entries))
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		assert.Nil(t, DuplicateStoreKeys(nil))
	})
}

func TestCombinedValue(t *testing.T) {
	value, err := CombinedValue([]byte("CERT"), []byte("KEY"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"certificate":"CERT","key":"KEY"}`, string(value))
}

func TestValidateX509KeyPair(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	t.Run("valid pair returns the leaf expiry", func(t *testing.T) {
		notAfter, err := ValidateX509KeyPair(certPEM, keyPEM)
		require.NoError(t, err)
		assert.True(t, notAfter.After(time.Now()), "generated cert must not be expired")
	})

	t.Run("mismatched key is rejected", func(t *testing.T) {
		_, otherKeyPEM := certificate.MustGenerateCertPEMFormat()
		_, err := ValidateX509KeyPair(certPEM, otherKeyPEM)
		require.Error(t, err)
	})

	t.Run("garbage is rejected", func(t *testing.T) {
		_, err := ValidateX509KeyPair([]byte("not a cert"), []byte("not a key"))
		require.Error(t, err)
	})

	t.Run("missing cert is rejected", func(t *testing.T) {
		_, err := ValidateX509KeyPair(nil, keyPEM)
		require.Error(t, err)
	})

	t.Run("missing key is rejected", func(t *testing.T) {
		_, err := ValidateX509KeyPair(certPEM, nil)
		require.Error(t, err)
	})

	t.Run("cert without its key is rejected", func(t *testing.T) {
		_, err := ValidateX509KeyPair(certPEM, certPEM)
		require.Error(t, err)
	})
}

func TestValueHash(t *testing.T) {
	hash := ValueHash([]byte("value"))
	assert.True(t, strings.HasPrefix(hash, "sha256:"))
	assert.Len(t, hash, len("sha256:")+64)
	assert.Equal(t, hash, ValueHash([]byte("value")), "deterministic")
	assert.NotEqual(t, hash, ValueHash([]byte("other")))
}

func TestReferenceSuffixes(t *testing.T) {
	t.Run("combined publishes one suffix per subfield", func(t *testing.T) {
		refs := ReferenceSuffixes(ResolvedEntry{
			StoreKey:  "k",
			SubFields: []string{CombinedCertificateSubField, CombinedKeySubField},
		})
		require.Len(t, refs, 2)
		assert.Equal(t, "k/certificate", refs[0].Suffix)
		assert.Equal(t, CombinedCertificateSubField, refs[0].SubField)
		assert.Equal(t, "k/key", refs[1].Suffix)
		assert.Equal(t, CombinedKeySubField, refs[1].SubField)
	})

	t.Run("split publishes the bare key", func(t *testing.T) {
		refs := ReferenceSuffixes(ResolvedEntry{StoreKey: "k"})
		require.Len(t, refs, 1)
		assert.Equal(t, "k", refs[0].Suffix)
		assert.Empty(t, refs[0].SubField)
	})
}
