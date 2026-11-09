package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The references by ID hold their id: an empty one marshals to {} only as
// the id is omitted when unset (see the generated MarshalEmpty tests).
func TestReferenceByIDHoldsItsID(t *testing.T) {
	t.Parallel()

	for name, ref := range map[string]any{
		"schema registry": &SchemaRegistryReferenceByID{},
		"static key":      &EncryptionKeyStaticReferenceByID{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, json.Unmarshal([]byte(`{"id":"9f3c2a1e-4b5d-4e6f-8a7b-0c1d2e3f4a5b"}`), ref))
			out, err := json.Marshal(ref)
			require.NoError(t, err)
			assert.JSONEq(t, `{"id":"9f3c2a1e-4b5d-4e6f-8a7b-0c1d2e3f4a5b"}`, string(out))
		})
	}
}
