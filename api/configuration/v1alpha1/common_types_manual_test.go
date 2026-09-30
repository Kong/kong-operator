package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFlattenSDKUnionsExceptUnder_ArrayDescentMultiSegmentPath covers array
// descent ("[]") and multi-segment freeform paths, mirroring the real
// AIGatewayMCPServer path ["conversion-listener", "tools", "[]", "headers"].
// The sibling string "headers" names the protected key, so without protection
// the union rewrite deletes the headers map and hoists its fields.
func TestFlattenSDKUnionsExceptUnder_ArrayDescentMultiSegmentPath(t *testing.T) {
	fields := []sdkOpsFreeformKeyField{
		{Path: []string{"conversion-listener", "tools", "[]", "headers"}},
	}
	in := map[string]any{
		"tools": []any{
			map[string]any{
				"name":    "headers",
				"headers": map[string]any{"x-a": "1"},
			},
		},
	}

	out, ok := flattenSDKUnionsExceptUnder(in, fields, "conversion-listener").(map[string]any)
	assert.True(t, ok)

	tools, ok := out["tools"].([]any)
	assert.True(t, ok)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool, ok := tools[0].(map[string]any)
	assert.True(t, ok)

	// The headers map must survive verbatim; the sibling string "headers"
	// must not trigger the union rewrite on the element map.
	assert.Equal(t, "headers", tool["name"])
	assert.Equal(t, map[string]any{"x-a": "1"}, tool["headers"])
}

// TestFlattenSensitiveDataExcept_FreeformScoping covers the freeform scoping
// of the sensitive-data collapse: a sensitive free-form leaf gets its own
// DataSource wrapper unwrapped, a non-sensitive free-form leaf keeps
// wrapper-shaped user data verbatim, and non-freeform subtrees keep the
// generic collapse.
func TestFlattenSensitiveDataExcept_FreeformScoping(t *testing.T) {
	fields := []sdkOpsFreeformKeyField{
		{Path: []string{"config"}, Sensitive: true},
		{Path: []string{"labels"}},
	}
	in := map[string]any{
		"config": map[string]any{
			"type":  "inline",
			"value": map[string]any{"a": 1},
		},
		"labels": map[string]any{"type": "inline", "value": "foo"},
		// Not a freeform path: the generic collapse still applies.
		"other": map[string]any{"type": "inline", "value": "z"},
		// An unresolved secretRef (no "value" key) stays unchanged.
		"ref": map[string]any{"type": "secretRef", "secretRef": map[string]any{"name": "s"}},
	}

	out, ok := flattenSensitiveDataExcept(in, fields).(map[string]any)
	assert.True(t, ok)

	assert.Equal(t, map[string]any{"a": 1}, out["config"])
	assert.Equal(t, map[string]any{"type": "inline", "value": "foo"}, out["labels"])
	assert.Equal(t, "z", out["other"])
	assert.Equal(t, map[string]any{"type": "secretRef", "secretRef": map[string]any{"name": "s"}}, out["ref"])
}
