package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// maxRequestBodySize documents 0 as "unlimited", so the generator emits it as
// *int (crd-from-oas `_allowZero`): an explicit 0 must survive the round trip
// instead of being dropped and replaced by the API's 8 MiB default.

func TestMaxRequestBodySize_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    *int
		wantJSON string
	}{
		{name: "unset is omitted", value: nil, wantJSON: `{}`},
		{name: "explicit zero is kept", value: new(0), wantJSON: `{"maxRequestBodySize":0}`},
		{name: "non-zero is kept", value: new(1024), wantJSON: `{"maxRequestBodySize":1024}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// One shared schema type per affected entity, plus the Agent's inline type.
			objs := map[string]any{
				"AIGatewayAgentConfig":                   &AIGatewayAgentConfig{MaxRequestBodySize: tc.value},
				"AIGatewayModelAPIConfig":                &AIGatewayModelAPIConfig{MaxRequestBodySize: tc.value},
				"AIGatewayMCPServerListenerConfig":       &AIGatewayMCPServerListenerConfig{MaxRequestBodySize: tc.value},
				"AIGatewayModelModelConfig":              &AIGatewayModelModelConfig{MaxRequestBodySize: tc.value},
				"AIGatewayMCPServerUpstreamServerConfig": &AIGatewayMCPServerUpstreamServerConfig{MaxRequestBodySize: tc.value},
			}
			for name, obj := range objs {
				data, err := json.Marshal(obj)
				require.NoError(t, err, name)

				var got map[string]any
				require.NoError(t, json.Unmarshal(data, &got), name)
				var want map[string]any
				require.NoError(t, json.Unmarshal([]byte(tc.wantJSON), &want))
				require.Equal(t, want["maxRequestBodySize"], got["maxRequestBodySize"], name)
			}

			var decoded AIGatewayModelAPIConfig
			require.NoError(t, json.Unmarshal([]byte(tc.wantJSON), &decoded))
			require.Equal(t, tc.value, decoded.MaxRequestBodySize)
		})
	}
}

func TestAIGatewayAgent_MaxRequestBodySize_Conversion(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	testCases := []struct {
		name  string
		value *int
	}{
		{name: "unset", value: nil},
		{name: "explicit zero", value: new(0)},
		{name: "non-zero", value: new(1024)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			obj := &AIGatewayAgent{
				Name: "sample-agent", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:   "sample-agent",
						Type:   "http",
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080", MaxRequestBodySize: tc.value},
					},
				},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()

			t.Run("Konnect create request", func(t *testing.T) {
				req, err := obj.ToCreateAIGatewayAgentRequest(t.Context(), cl)
				require.NoError(t, err)
				require.NotNil(t, req.Config.MaxRequestBodySize)
				if tc.value == nil {
					// Unset falls back to the SDK's default (8 MiB), not to 0 (unlimited).
					require.EqualValues(t, 8388608, *req.Config.MaxRequestBodySize)
					return
				}
				require.EqualValues(t, *tc.value, *req.Config.MaxRequestBodySize)
			})

			t.Run("on-prem aigw agent", func(t *testing.T) {
				agent, err := obj.ToAIGWAgent(t.Context(), cl)
				require.NoError(t, err)
				require.Equal(t, tc.value, agent.Config.MaxRequestBodySize)
			})
		})
	}
}
