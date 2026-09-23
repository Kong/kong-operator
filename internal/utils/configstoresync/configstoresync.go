// Package configstoresync holds pure helpers shared by the
// KonnectConfigStoreSync controller and the conflict field indexer: store key
// derivation, entry resolution, value building and hashing. Keeping them here
// (instead of in controller/konnect) lets internal/utils/index import them
// without creating an import cycle.
package configstoresync

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"time"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// MaxKeyBytes is the Konnect Config Store key size cap (512 bytes).
	// Explicit storeKeys are capped by CRD validation, but derived keys are
	// not: a 63-char namespace, 253-char name and 253-char Split field derive
	// a ~582-byte key. The controller enforces this cap pre-write and reports
	// KeyTooLong instead of pushing.
	MaxKeyBytes = 512
	// MaxValueBytes is the Konnect Config Store value size cap (5120 bytes).
	MaxValueBytes = 5120

	// CombinedCertificateSubField is the frozen JSON subfield holding the
	// certificate in a Combined mode entry value.
	CombinedCertificateSubField = "certificate"
	// CombinedKeySubField is the frozen JSON subfield holding the private key
	// in a Combined mode entry value.
	CombinedKeySubField = "key"
)

// DerivedKey returns the store key derived from the sync's identity using the
// length-prefixed format k8s-<len(namespace)>-<namespace>-<len(name)>-<name>.
// The length prefixes make the encoding unambiguous: no two distinct
// (namespace, name) pairs can produce the same derived key.
func DerivedKey(namespace, name string) string {
	return fmt.Sprintf("k8s-%d-%s-%d-%s", len(namespace), namespace, len(name), name)
}

// ResolvedEntry is one Config Store entry a sync manages, with its resolved
// store key and the Secret data fields that produce its value.
type ResolvedEntry struct {
	// StoreKey is the resolved Config Store entry key (explicit or derived).
	StoreKey string
	// SourceFields are the Secret data keys whose values produce the value.
	SourceFields []string
	// SubFields are the JSON subfields of the stored value each source field
	// maps to. It is empty for Split mode entries (raw values).
	SubFields []string
}

// ResolveEntries computes the set of Config Store entries a sync manages from
// its spec alone (no Secret or store data needed), applying key derivation
// where no explicit storeKey is set.
//
// A Split entry's explicit storeKey may still equal another entry's derived
// key: CRD validation checks uniqueness of explicit storeKeys only, so the
// caller must reject duplicate resolved StoreKeys itself (see
// DuplicateStoreKeys).
func ResolveEntries(sync *konnectv1alpha1.KonnectConfigStoreSync) []ResolvedEntry {
	derived := DerivedKey(sync.Namespace, sync.Name)
	switch sync.Spec.Mode {
	case konnectv1alpha1.KonnectConfigStoreSyncModeSplit:
		if sync.Spec.Split == nil {
			return nil
		}
		entries := make([]ResolvedEntry, 0, len(sync.Spec.Split.Entries))
		for _, e := range sync.Spec.Split.Entries {
			storeKey := derived + "-" + e.Field
			if e.StoreKey != nil && *e.StoreKey != "" {
				storeKey = *e.StoreKey
			}
			entries = append(entries, ResolvedEntry{
				StoreKey:     storeKey,
				SourceFields: []string{e.Field},
			})
		}
		return entries
	default: // Combined
		storeKey := derived
		certField, keyField := "tls.crt", "tls.key"
		if sync.Spec.Combined != nil {
			if sync.Spec.Combined.StoreKey != nil && *sync.Spec.Combined.StoreKey != "" {
				storeKey = *sync.Spec.Combined.StoreKey
			}
			if sync.Spec.Combined.CertificateField != "" {
				certField = sync.Spec.Combined.CertificateField
			}
			if sync.Spec.Combined.KeyField != "" {
				keyField = sync.Spec.Combined.KeyField
			}
		}
		return []ResolvedEntry{
			{
				StoreKey:     storeKey,
				SourceFields: []string{certField, keyField},
				SubFields:    []string{CombinedCertificateSubField, CombinedKeySubField},
			},
		}
	}
}

// DuplicateStoreKeys returns the store keys claimed by more than one resolved
// entry, or nil when all keys are distinct. Status entries are list-keyed by
// storeKey, so duplicates would both double-write the same Config Store entry
// and be rejected by the API server on status update.
func DuplicateStoreKeys(entries []ResolvedEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	var duplicates []string
	for _, e := range entries {
		if _, ok := seen[e.StoreKey]; ok {
			duplicates = append(duplicates, e.StoreKey)
			continue
		}
		seen[e.StoreKey] = struct{}{}
	}
	return duplicates
}

// combinedValue is the frozen JSON shape of a Combined mode entry value. The
// subfield names are part of the API contract: they appear verbatim in vault
// reference strings and must never be renamed.
type combinedValue struct {
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// CombinedValue builds the Combined mode entry value from the certificate and
// key PEM data.
func CombinedValue(certPEM, keyPEM []byte) ([]byte, error) {
	return json.Marshal(combinedValue{
		Certificate: string(certPEM),
		Key:         string(keyPEM),
	})
}

// ValidateX509KeyPair validates that certPEM and keyPEM form a valid x509
// certificate/key pair, following the [tls.X509KeyPair] pattern used by the
// ingress-controller translator. It returns the leaf certificate's NotAfter
// expiry. A missing or unparsable cert or key fails validation: there is no
// bypass.
func ValidateX509KeyPair(certPEM, keyPEM []byte) (notAfter time.Time, err error) {
	certPEM = bytes.TrimSpace(certPEM)
	keyPEM = bytes.TrimSpace(keyPEM)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing TLS key-pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return time.Time{}, fmt.Errorf("parsing TLS key-pair: no certificate found")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing leaf certificate: %w", err)
	}
	return leaf.NotAfter, nil
}

// ValueHash returns the "sha256:<hex>" hash of a value. Status, logs and
// events only ever carry this hash, never Secret plaintext.
func ValueHash(value []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(value))
}

// ReferenceSuffixes returns the status.references entries for a resolved
// entry: "<storeKey>/<subfield>" per subfield for Combined mode entries, or
// "<storeKey>" for Split mode entries.
func ReferenceSuffixes(entry ResolvedEntry) []konnectv1alpha1.KonnectConfigStoreSyncReference {
	if len(entry.SubFields) == 0 {
		return []konnectv1alpha1.KonnectConfigStoreSyncReference{
			{Suffix: entry.StoreKey},
		}
	}
	refs := make([]konnectv1alpha1.KonnectConfigStoreSyncReference, 0, len(entry.SubFields))
	for _, sub := range entry.SubFields {
		refs = append(refs, konnectv1alpha1.KonnectConfigStoreSyncReference{
			SubField: sub,
			Suffix:   entry.StoreKey + "/" + sub,
		})
	}
	return refs
}
