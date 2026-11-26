package ops

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// SyncHashKeyMinLength is the minimum length in bytes of a key used to
	// compute keyed sync hashes. It matches the SHA-256 block output size so the
	// key has at least 256 bits of entropy when it is generated randomly.
	SyncHashKeyMinLength = 32

	// syncHashKeyIDLength is the number of hex characters of a key ID.
	syncHashKeyIDLength = 16

	// syncHashKeyIDContext is the domain separation string used to derive a key
	// ID from a key, so the ID never equals a digest of entity data.
	syncHashKeyIDContext = "kong-operator/konnect-sync-hash-key-id"
)

// SyncHashKey is a secret key used to compute keyed HMAC-SHA256 hashes of
// secret-bearing Kong Gateway entities persisted in status.sync.
type SyncHashKey struct {
	id     string
	secret []byte
}

// NewSyncHashKey returns a SyncHashKey for the given key material.
// The key material must be at least SyncHashKeyMinLength bytes long.
func NewSyncHashKey(secret []byte) (SyncHashKey, error) {
	if len(secret) < SyncHashKeyMinLength {
		return SyncHashKey{}, fmt.Errorf(
			"sync hash key must be at least %d bytes long, got %d", SyncHashKeyMinLength, len(secret),
		)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(syncHashKeyIDContext))
	return SyncHashKey{
		id:     hex.EncodeToString(mac.Sum(nil))[:syncHashKeyIDLength],
		secret: append([]byte(nil), secret...),
	}, nil
}

// GenerateSyncHashKeyMaterial returns new random key material for a SyncHashKey.
func GenerateSyncHashKeyMaterial() ([]byte, error) {
	b := make([]byte, SyncHashKeyMinLength)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed generating sync hash key: %w", err)
	}
	return b, nil
}

// ID returns the identifier of the key persisted in status.sync.keyID. It is
// derived from the key with HMAC-SHA256 and does not reveal the key.
func (k SyncHashKey) ID() string {
	return k.id
}

// SyncHashKeys is the set of keys used to compute keyed sync hashes.
type SyncHashKeys struct {
	// Current is the key used to compute new hashes.
	Current SyncHashKey
	// Previous, when set, is the key that was current before a rotation.
	// Hashes computed with it are verified with a GET and rewritten with the
	// current key without sending the entity to Konnect.
	Previous *SyncHashKey
}

// SetSyncHashKeys sets the keys used to compute keyed HMAC-SHA256 hashes of
// secret-bearing entities. Without keys, the sync state of secret-bearing
// entities is tracked in memory only and never persisted in status.
//
// It must be called before the tracker is used.
func (t *ConfigVersionTracker) SetSyncHashKeys(keys SyncHashKeys) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current := keys.Current
	t.hashKeys.current = &current
	t.hashKeys.previous = nil
	if keys.Previous != nil && keys.Previous.id != current.id {
		previous := *keys.Previous
		t.hashKeys.previous = &previous
	}
}

type syncHashKeySet struct {
	current  *SyncHashKey
	previous *SyncHashKey
}

func (t *ConfigVersionTracker) syncHashKeys() syncHashKeySet {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hashKeys
}

// isSecretBearingSyncEntity reports whether the payload sent to Konnect for the
// entity can carry secret material read from a Secret that is not readable from
// the entity itself. The sync hashes of such entities are keyed HMAC-SHA256
// digests, so status.sync does not expose more than the entity already does.
//
//   - KongPluginBinding: plugin configuration can be sourced from Secrets through
//     the KongPlugin configFrom and configPatches fields.
//   - KongCertificate: the certificate private key can be read from a Secret
//     through secretRef and secretRefAlt. Inline certificates are keyed too, so
//     the hash kind does not depend on the source type.
//
// Credentials and KongKey are not secret-bearing in this sense: their secret is
// part of the entity spec, also when it is copied from a KongConsumer
// credential Secret, so anyone who can read their status can read it already.
func isSecretBearingSyncEntity(e any) bool {
	switch e.(type) {
	case *configurationv1alpha1.KongPluginBinding,
		*configurationv1alpha1.KongCertificate:
		return true
	default:
		return false
	}
}
