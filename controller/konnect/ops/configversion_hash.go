package ops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
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

	hashPrefix = "sha256:"
)

type hash [sha256.Size]byte

func (h hash) String() string {
	return hashPrefix + hex.EncodeToString(h[:])
}

func parseHash(s string) (hash, bool) {
	if !strings.HasPrefix(s, hashPrefix) {
		return hash{}, false
	}
	b, err := hex.DecodeString(strings.TrimPrefix(s, hashPrefix))
	if err != nil || len(b) != sha256.Size {
		return hash{}, false
	}
	var h hash
	copy(h[:], b)
	return h, true
}

type canonicalizeMode int

const (
	canonicalizeModePayload canonicalizeMode = iota
	canonicalizeModeRepresentation
)

// entityPayloadHash hashes the canonical JSON of the desired Konnect request
// together with the control plane and entity identifiers so changes to either
// invalidate the cached hash.
func entityPayloadHash(controlPlaneID, konnectID string, payload any) (hash, error) {
	b, err := canonicalPayloadJSON(controlPlaneID, konnectID, payload)
	if err != nil {
		return hash{}, err
	}
	return sha256.Sum256(b), nil
}

// representationHash hashes the canonical JSON of the Konnect representation of
// an entity, ignoring volatile server-managed fields and non-guaranteed set order.
func representationHash(v any) (hash, error) {
	b, err := canonicalRepresentationJSON(v)
	if err != nil {
		return hash{}, err
	}
	return sha256.Sum256(b), nil
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
// canonical JSON: object keys are sorted recursively, and volatile
// server-managed fields are removed.
func canonicalRepresentationJSON(v any) ([]byte, error) {
	return canonicalizeJSON(v, canonicalizeModeRepresentation)
}

func canonicalizeJSON(v any, mode canonicalizeMode) ([]byte, error) {
	decoded, err := decodeJSONValue(v)
	if err != nil {
		return nil, err
	}
	canonical := canonicalizeValue(decoded, mode, false)
	return json.Marshal(canonical)
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

func canonicalizeValue(v any, mode canonicalizeMode, inOpaqueConfig bool) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			if mode == canonicalizeModeRepresentation && !inOpaqueConfig && slices.Contains(volatileRepresentationFields, k) {
				continue
			}
			// Plugin "config" maps hold arbitrary plugin-specific schemas: keep
			// recursive key sorting, but do not strip fields inside them, as a
			// plugin config key named like a volatile field is regular data.
			childOpaque := inOpaqueConfig || k == "config"
			out[k] = canonicalizeValue(child, mode, childOpaque)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = canonicalizeValue(child, mode, inOpaqueConfig)
		}
		return out
	default:
		return value
	}
}

// volatileRepresentationFields are the fields of a Konnect entity
// representation that Konnect changes without the entity changing. They are
// removed, at any depth outside plugin configs, before hashing a representation.
//
// Comparing the PUT and GET responses of every guarded entity type against
// Konnect showed no other such field. A missing entry costs a PUT whenever the
// entity is verified with a GET, not a missed change.
var volatileRepresentationFields = []string{
	"created_at",
	"updated_at",
}
