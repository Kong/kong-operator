package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAIGatewayCustomPolicyAPISpec_LabelsAndManagedByReachSDKVerbatim checks
// that labels and managedBy, set on either union variant, are carried into the
// SDK create/update requests with their user-provided keys untouched (i.e. not
// camelCase→snake_case renamed or collapsed by the union flattening).
func TestAIGatewayCustomPolicyAPISpec_LabelsAndManagedByReachSDKVerbatim(t *testing.T) {
	labels := PublicLabels{"teamName": "ai", "cost-center": "1234"}
	managedBy := ManagedBy{"toolName": "kong-operator", "env": "prod"}
	wantLabels := map[string]string{"teamName": "ai", "cost-center": "1234"}
	wantManagedBy := map[string]string{"toolName": "kong-operator", "env": "prod"}

	t.Run("installed", func(t *testing.T) {
		spec := &AIGatewayCustomPolicyAPISpec{
			AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
				Type: AIGatewayCustomPolicyConfigTypeInstalled,
				Installed: &CreateAIGatewayCustomPolicyInstalledRequest{
					Name:        "my-installed-policy",
					DisplayName: "My installed policy",
					Schema:      "return {}",
					Labels:      labels,
					ManagedBy:   managedBy,
				},
			},
		}

		createReq, err := spec.ToCreateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, createReq.CreateAIGatewayCustomPolicyInstalledRequest)
		assert.Equal(t, wantLabels, createReq.CreateAIGatewayCustomPolicyInstalledRequest.GetLabels())
		assert.Equal(t, wantManagedBy, createReq.CreateAIGatewayCustomPolicyInstalledRequest.GetManagedBy())

		updateReq, err := spec.ToUpdateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest)
		assert.Equal(t, wantLabels, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest.GetLabels())
		assert.Equal(t, wantManagedBy, updateReq.UpdateAIGatewayCustomPolicyInstalledRequest.GetManagedBy())
	})

	t.Run("streaming", func(t *testing.T) {
		spec := &AIGatewayCustomPolicyAPISpec{
			AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
				Type: AIGatewayCustomPolicyConfigTypeStreaming,
				Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
					Name:        "my-streaming-policy",
					DisplayName: "My streaming policy",
					Schema:      "return {}",
					Handler:     "return {}",
					Labels:      labels,
					ManagedBy:   managedBy,
				},
			},
		}

		createReq, err := spec.ToCreateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, createReq.CreateAIGatewayCustomPolicyStreamingRequest)
		assert.Equal(t, wantLabels, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetLabels())
		assert.Equal(t, wantManagedBy, createReq.CreateAIGatewayCustomPolicyStreamingRequest.GetManagedBy())

		updateReq, err := spec.ToUpdateAIGatewayCustomPolicyRequest()
		require.NoError(t, err)
		require.NotNil(t, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest)
		assert.Equal(t, wantLabels, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetLabels())
		assert.Equal(t, wantManagedBy, updateReq.UpdateAIGatewayCustomPolicyStreamingRequest.GetManagedBy())
	})
}
