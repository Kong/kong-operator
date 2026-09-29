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
	t.Run("matches by Kubernetes UID label", func(t *testing.T) {
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
							// Same name but no UID label: must not be adopted.
							AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
								ID:   "same-name-no-label",
								Name: "my-streaming-policy",
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming,
						},
						{
							AIGatewayCustomPolicyInstalled: &sdkkonnectcomp.AIGatewayCustomPolicyInstalled{
								ID:     "other-uid",
								Name:   "other-policy",
								Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"},
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeInstalled,
						},
						{
							AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
								ID:     "matched-by-uid",
								Name:   "my-streaming-policy",
								Labels: map[string]string{KubernetesUIDLabelKey: string(policy.GetUID())},
							},
							Type: sdkkonnectcomp.AIGatewayCustomPolicyTypeStreaming,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayCustomPolicyForUID(ctx, sdk, policy)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-uid", id)
	})

	// Regression: a second CR declaring the same name as an existing one gets
	// a 409 on create. The conflict lookup must not adopt the entity owned by
	// the other CR, otherwise both CRs would share (and overwrite, and on
	// deletion remove) the same Konnect entity.
	t.Run("does not adopt a same-named policy owned by another CR", func(t *testing.T) {
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
								ID:     "owned-by-other-cr",
								Name:   "my-streaming-policy",
								Labels: map[string]string{KubernetesUIDLabelKey: "other-cr-uid"},
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

	t.Run("returns not found without listing when the object has no UID", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policy := testAIGatewayCustomPolicy()
		policy.UID = ""

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
						Schema:      aiconfigurationv1alpha1.ConfigMapDataSource{Type: aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline, Value: new("return {}")},
						Handler:     aiconfigurationv1alpha1.ConfigMapDataSource{Type: aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline, Value: new("return {}")},
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
