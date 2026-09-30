package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// TestAIGatewayPolicyAPISpec_MarshalKeepsFreeformConfigVerbatim is a
// regression test for the flattenSDKUnions union heuristic: it used to fire on
// free-form config whose user data merely looks like a discriminated union
// (e.g. a headroom compressor config with provider: headroom next to a
// headroom: {...} block), hoisting the nested block into config. Konnect then
// rejected the flattened payload with "unknown field" errors. Free-form
// config is user data and must reach Konnect verbatim.
func TestAIGatewayPolicyAPISpec_MarshalKeepsFreeformConfigVerbatim(t *testing.T) {
	configJSON := `{
		"provider": "headroom",
		"compressor_url": "http://headroom.default.svc.cluster.local:8787",
		"compressor_type": "rate",
		"message_type": ["user"],
		"stop_on_error": false,
		"headroom": {
			"proxy_token": "test-token",
			"ssl_verify": false,
			"session_id_headers": ["x-session-id"]
		},
		"headers": [
			{"type": "header", "header": "x-foo"},
			{"type": "header", "header": "x-bar", "value": "baz"}
		]
	}`
	spec := &AIGatewayPolicyAPISpec{
		Name: "headroom-compressor",
		Type: "ai-prompt-compressor",
		Config: AIGatewayPolicyConfigDataSource{
			Type:  "inline",
			Value: &apiextensionsv1.JSON{Raw: []byte(configJSON)},
		},
		Labels: PublicLabels{"provider": "headroom", "headroom": "x"},
	}

	data, err := spec.marshalSDKOpsPayload()
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))

	var wantConfig map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &wantConfig))
	// config must reach the SDK verbatim: no hoisted headroom fields, no
	// collapsed {"type": "header", "header": ...} objects.
	assert.Equal(t, wantConfig, payload["config"])
	// labels is free-form too: a two-key map must not collapse into a scalar.
	assert.Equal(t, map[string]any{"provider": "headroom", "headroom": "x"}, payload["labels"])
}

// TestAIGatewayPolicyAPISpec_MarshalKeepsConfigWhenSiblingNamesIt is a
// regression test for the parent-map rewrite: a sibling string field whose
// value names the free-form key (here name: "config") used to make the union
// heuristic fire on the payload root, deleting the config key and hoisting
// its contents before flattenSensitiveData collapsed the whole payload.
func TestAIGatewayPolicyAPISpec_MarshalKeepsConfigWhenSiblingNamesIt(t *testing.T) {
	configJSON := `{
		"provider": "headroom",
		"headroom": {
			"proxy_token": "test-token",
			"ssl_verify": false,
			"session_id_headers": ["x-session-id"]
		}
	}`
	spec := &AIGatewayPolicyAPISpec{
		Name: "config",
		Type: "ai-prompt-compressor",
		Config: AIGatewayPolicyConfigDataSource{
			Type:  "inline",
			Value: &apiextensionsv1.JSON{Raw: []byte(configJSON)},
		},
		Labels: PublicLabels{"provider": "headroom", "headroom": "x"},
	}

	data, err := spec.marshalSDKOpsPayload()
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))

	var wantConfig map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &wantConfig))
	assert.Equal(t, "config", payload["name"])
	assert.Equal(t, wantConfig, payload["config"])
	assert.Equal(t, map[string]any{"provider": "headroom", "headroom": "x"}, payload["labels"])
}

// TestAIGatewayPolicyAPISpec_MarshalDoesNotCollapseWrapperShapedUserData is a
// regression test for flattenSensitiveData: the generic collapse used to
// rewrite user data inside free-form subtrees too — labels of shape
// {"type": "inline", "value": "foo"} collapsed to "foo", and nested
// inline-shaped objects inside config were corrupted. Only the free-form
// leaf that is itself a secretReference target (config) gets its own
// DataSource wrapper unwrapped.
func TestAIGatewayPolicyAPISpec_MarshalDoesNotCollapseWrapperShapedUserData(t *testing.T) {
	configJSON := `{
		"provider": "headroom",
		"nested": {"type": "inline", "value": "x"}
	}`
	spec := &AIGatewayPolicyAPISpec{
		Name: "headroom-compressor",
		Type: "ai-prompt-compressor",
		Config: AIGatewayPolicyConfigDataSource{
			Type:  "inline",
			Value: &apiextensionsv1.JSON{Raw: []byte(configJSON)},
		},
		Labels:    PublicLabels{"type": "inline", "value": "foo"},
		ManagedBy: ManagedBy{"type": "inline", "value": "bar"},
	}

	data, err := spec.marshalSDKOpsPayload()
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))

	// config's own DataSource wrapper is unwrapped, but its contents stay
	// verbatim: the nested inline-shaped object must not be collapsed.
	var wantConfig map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &wantConfig))
	assert.Equal(t, wantConfig, payload["config"])
	// Wrapper-shaped label/managed-by user data must survive verbatim.
	assert.Equal(t, map[string]any{"type": "inline", "value": "foo"}, payload["labels"])
	assert.Equal(t, map[string]any{"type": "inline", "value": "bar"}, payload["managed_by"])
}
