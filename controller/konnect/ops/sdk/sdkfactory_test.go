package sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeGRPCRouteRequestBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		expectPatch bool
		assertions  func(t *testing.T, payload map[string]any)
	}{
		{
			name: "grpc route removes strip_path",
			body: `{
				"name":"grpc-route",
				"protocols":["grpc","grpcs"],
				"paths":["~/foo.Service/Bar$"],
				"strip_path":true,
				"preserve_host":false
			}`,
			expectPatch: true,
			assertions: func(t *testing.T, payload map[string]any) {
				assert.NotContains(t, payload, "strip_path")
				assert.Equal(t, false, payload["preserve_host"])
			},
		},
		{
			name: "http route remains unchanged",
			body: `{
				"name":"http-route",
				"protocols":["http","https"],
				"paths":["/"],
				"strip_path":true
			}`,
			expectPatch: false,
			assertions: func(t *testing.T, payload map[string]any) {
				assert.Equal(t, true, payload["strip_path"])
			},
		},
		{
			name: "grpc route without strip_path remains unchanged",
			body: `{
				"name":"grpc-route",
				"protocols":["grpc"],
				"paths":["~/foo.Service/Bar$"]
			}`,
			expectPatch: false,
			assertions: func(t *testing.T, payload map[string]any) {
				assert.NotContains(t, payload, "strip_path")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, patched, err := sanitizeGRPCRouteRequestBody([]byte(tt.body))
			require.NoError(t, err)
			assert.Equal(t, tt.expectPatch, patched)

			var payload map[string]any
			require.NoError(t, json.Unmarshal(sanitized, &payload))
			tt.assertions(t, payload)
		})
	}
}
