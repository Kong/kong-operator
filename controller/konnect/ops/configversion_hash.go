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
// sorted, numbers keep exact JSON encoding, and known unordered string-set
// fields are sorted.
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
// canonical JSON: object keys are sorted recursively, volatile server-managed
// fields are removed, and known unordered string-set fields are sorted.
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
			if mode == canonicalizeModeRepresentation && !inOpaqueConfig && isVolatileRepresentationField(k) {
				continue
			}
			// Plugin "config" maps hold arbitrary plugin-specific schemas: keep
			// recursive key sorting, but do not strip fields or reorder arrays
			// inside them because array order and field semantics are plugin-defined.
			childOpaque := inOpaqueConfig || k == "config"
			if !childOpaque && isUnorderedStringSetField(k) {
				if sorted, ok := sortStringSlice(child); ok {
					out[k] = sorted
					continue
				}
			}
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

func isVolatileRepresentationField(key string) bool {
	switch key {
	case "created_at", "updated_at":
		return true
	default:
		return false
	}
}

// isUnorderedStringSetField reports whether key names a Kong Gateway entity
// field whose elements form an unordered set of strings (for example, Konnect
// actively reorders "protocols" on Route and Plugin responses).
func isUnorderedStringSetField(key string) bool {
	switch key {
	case "tags", "protocols", "ca_certificates", "group_ids":
		return true
	default:
		return false
	}
}

func sortStringSlice(v any) ([]any, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	strs := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		strs[i] = s
	}
	slices.Sort(strs)
	out := make([]any, len(strs))
	for i, s := range strs {
		out[i] = s
	}
	return out, true
}
