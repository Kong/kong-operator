package ops

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const (
	// SyncHashVersion is the version of the payload and representation hash
	// scheme persisted in status.sync.
	//
	// Bump it whenever canonicalization, hashing, or SDK JSON serialization
	// changes the computed hashes for any guarded entity type (caught by
	// TestEntitySyncHashesPerType). Entities whose persisted hashes carry a
	// different version are treated as unverified and sent to Konnect once more.
	//
	// See docs/internal/konnect-sync-hashing.md for the hashing and migration contract.
	SyncHashVersion = "v1"

	hashPrefix     = "sha256:"
	hmacHashPrefix = "hmac-sha256:"
)

type hash [sha256.Size]byte

// String returns the unkeyed SHA-256 encoding of the hash.
func (h hash) String() string {
	return hashPrefix + hex.EncodeToString(h[:])
}

// format returns the status encoding of a hash computed by a hasher: keyed
// digests use the hmac-sha256 prefix so they are never mistaken for, or
// compared with, unkeyed SHA-256 digests.
func (h hash) format(keyed bool) string {
	if keyed {
		return hmacHashPrefix + hex.EncodeToString(h[:])
	}
	return h.String()
}

// parseHash parses a hash encoded by hash.format. keyed reports whether it is
// a keyed HMAC-SHA256 digest.
func parseHash(s string) (h hash, keyed bool, ok bool) {
	var encoded string
	switch {
	case strings.HasPrefix(s, hmacHashPrefix):
		keyed, encoded = true, strings.TrimPrefix(s, hmacHashPrefix)
	case strings.HasPrefix(s, hashPrefix):
		encoded = strings.TrimPrefix(s, hashPrefix)
	default:
		return hash{}, false, false
	}
	b, err := hex.DecodeString(encoded)
	if err != nil || len(b) != sha256.Size {
		return hash{}, false, false
	}
	copy(h[:], b)
	return h, keyed, true
}

// syncHasher computes the digest of canonical JSON.
//
// The zero value computes an unkeyed SHA-256 digest. A hasher built from a
// SyncHashKey computes a keyed HMAC-SHA256 digest, which is used for entities
// whose payload or Konnect representation carries secret material, so a hash
// readable in status cannot be used to brute force a low-entropy secret
// offline.
type syncHasher struct {
	key *SyncHashKey
}

// keyed reports whether the hasher computes keyed HMAC-SHA256 digests.
func (h syncHasher) keyed() bool {
	return h.key != nil
}

// keyID returns the ID of the hasher's key, or an empty string for an unkeyed hasher.
func (h syncHasher) keyID() string {
	if h.key == nil {
		return ""
	}
	return h.key.id
}

func (h syncHasher) sum(b []byte) hash {
	if h.key == nil {
		return sha256.Sum256(b)
	}
	mac := hmac.New(sha256.New, h.key.secret)
	mac.Write(b)
	var out hash
	copy(out[:], mac.Sum(nil))
	return out
}

type canonicalizeMode int

const (
	canonicalizeModePayload canonicalizeMode = iota
	canonicalizeModeRepresentation
)

// entityPayloadHash hashes the canonical JSON of the desired Konnect request
// together with the control plane and entity identifiers so changes to either
// invalidate the cached hash.
func entityPayloadHash(h syncHasher, controlPlaneID, konnectID string, payload any) (hash, error) {
	b, err := canonicalPayloadJSON(controlPlaneID, konnectID, payload)
	if err != nil {
		return hash{}, err
	}
	return h.sum(b), nil
}

// representationHash hashes the canonical JSON of the Konnect representation of
// an entity, ignoring volatile server-managed fields and non-guaranteed set order.
func representationHash(h syncHasher, v any) (hash, error) {
	b, err := canonicalRepresentationJSON(v)
	if err != nil {
		return hash{}, err
	}
	return h.sum(b), nil
}

// canonicalPayloadJSON encodes the desired Konnect request envelope into
// canonical JSON: every JSON object at every depth has its keys recursively
// sorted, and numbers keep their exact JSON encoding.
func canonicalPayloadJSON(controlPlaneID, konnectID string, payload any) ([]byte, error) {
	envelope := struct {
		ControlPlaneID string `json:"control_plane_id"`
		ID             string `json:"id"`
		Payload        any    `json:"payload"`
	}{
		ControlPlaneID: controlPlaneID,
		ID:             konnectID,
		Payload:        payload,
	}
	return canonicalizeJSON(envelope, canonicalizeModePayload)
}

// canonicalRepresentationJSON encodes a Konnect entity representation into
// canonical JSON: object keys are sorted recursively, and the volatile
// server-managed fields of the entity are removed from the top-level object.
func canonicalRepresentationJSON(v any) ([]byte, error) {
	return canonicalizeJSON(v, canonicalizeModeRepresentation)
}

func canonicalizeJSON(v any, mode canonicalizeMode) ([]byte, error) {
	decoded, err := decodeJSONValue(v)
	if err != nil {
		return nil, err
	}
	if obj, ok := decoded.(map[string]any); ok && mode == canonicalizeModeRepresentation {
		// Volatile fields are server-managed fields of the entity itself, so
		// they are only removed from the top-level object. A nested key with
		// the same name is user data, for example a route header or a plugin
		// config field, and a change to it must be detected.
		for _, field := range volatileRepresentationFields {
			delete(obj, field)
		}
	}
	// encoding/json sorts the keys of the decoded maps at every depth, and
	// json.Number keeps the exact encoding of numbers.
	return json.Marshal(decoded)
}

func decodeJSONValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// volatileRepresentationFields are the top-level fields of a Konnect entity
// representation that Konnect changes without the entity changing. They are
// removed from the top-level object before hashing a representation; no
// guarded representation type has such fields below the top level (see
// TestRepresentationTypesHaveTopLevelTimestampsOnly).
//
// Comparing the PUT and GET responses of every guarded entity type against
// Konnect showed no other such field. A missing entry costs a PUT whenever the
// entity is verified with a GET, not a missed change.
var volatileRepresentationFields = []string{
	"created_at",
	"updated_at",
}
