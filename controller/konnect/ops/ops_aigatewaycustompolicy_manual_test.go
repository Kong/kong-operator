package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestGetAIGatewayCustomPolicyForUID(t *testing.T) {
	t.Run("matches by type and name", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policy := testAIGatewayCustomPolicy()

		sdk.EXPECT().
			ListAiGatewayCustomPolicies(mock.Anything, sdkkonnectops.ListAiGatewayCustomPoliciesRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayCustomPoliciesResponse{
				ListAIGatewayCustomPoliciesResponse: &sdkkonnectcomp.ListAIGatewayCustomPoliciesResponse{
					Data: []sdkkonnectcomp.AIGatewayCustomPolicy{
						{
							AIGatewayCustomPolicyInstalled: &sdkkonnectcomp.AIGatewayCustomPolicyInstalled{
								ID:   "wrong-variant",
								Name: "my-streaming-policy",
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeInstalled,
						},
						{
							AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
								ID:   "other-id",
								Name: "other-policy",
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming,
						},
						{
							AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
								ID:   "matched-by-name",
								Name: "my-streaming-policy",
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayCustomPolicyForUID(ctx, sdk, policy)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-name", id)
	})

	t.Run("returns not found when no matching entry exists", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policy := testAIGatewayCustomPolicy()

		sdk.EXPECT().
			ListAiGatewayCustomPolicies(mock.Anything, sdkkonnectops.ListAiGatewayCustomPoliciesRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayCustomPoliciesResponse{
				ListAIGatewayCustomPoliciesResponse: &sdkkonnectcomp.ListAIGatewayCustomPoliciesResponse{
					Data: []sdkkonnectcomp.AIGatewayCustomPolicy{
						{
							AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
								ID:   "other-id",
								Name: "other-policy",
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayCustomPolicyForUID(ctx, sdk, policy)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("requires parent gateway ID", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policy := testAIGatewayCustomPolicy()
		policy.Status.GatewayID = nil

		id, err := getAIGatewayCustomPolicyForUID(ctx, sdk, policy)
		require.Empty(t, id)

		var parentErr CantPerformOperationWithoutParentIDError
		require.ErrorAs(t, err, &parentErr)
	})
}

func testAIGatewayCustomPolicy() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
	return &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
		Kind:       "AIGatewayCustomPolicy",
		Name:       "aigatewaycustompolicy",
		Namespace:  "default",
		UID:        "aigatewaycustompolicy-uid",
		Generation: 2,
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "ai-gw-cp-1",
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
					Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
						Name:        "my-streaming-policy",
						DisplayName: "My streaming policy",
						Schema:      "return {}",
						Handler:     "return {}",
					},
				},
			},
		},
		Status: aiconfigurationv1alpha1.AIGatewayCustomPolicyStatus{
			GatewayID: &aiconfigurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
		},
	}
}
