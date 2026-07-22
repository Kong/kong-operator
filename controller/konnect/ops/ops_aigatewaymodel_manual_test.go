package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestGetAIGatewayModelForUID(t *testing.T) {
	t.Run("matches by kubernetes UID label when present", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayModelsSDK(t)
		model := testAIGatewayModel()

		sdk.EXPECT().
			ListAiGatewayModels(mock.Anything, sdkkonnectops.ListAiGatewayModelsRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayModelsResponse{
				ListAIGatewayModelsResponse: &sdkkonnectcomp.ListAIGatewayModelsResponse{
					Data: []sdkkonnectcomp.AIGatewayModel{
						{
							AIGatewayModelAIGatewayModelAPI: &sdkkonnectcomp.AIGatewayModelAIGatewayModelAPI{
								ID:     "other-id",
								Name:   "other-model",
								Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"},
							},
							Type: sdkkonnectcomp.AIGatewayModelTypeAPI,
						},
						{
							AIGatewayModelAIGatewayModelAPI: &sdkkonnectcomp.AIGatewayModelAIGatewayModelAPI{
								ID:     "matched-by-uid",
								Name:   "different-name",
								Labels: map[string]string{KubernetesUIDLabelKey: string(model.GetUID())},
							},
							Type: sdkkonnectcomp.AIGatewayModelTypeAPI,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayModelForUID(ctx, sdk, model)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-uid", id)
	})

	t.Run("falls back to matching by type and name", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayModelsSDK(t)
		model := testAIGatewayModel()

		sdk.EXPECT().
			ListAiGatewayModels(mock.Anything, sdkkonnectops.ListAiGatewayModelsRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayModelsResponse{
				ListAIGatewayModelsResponse: &sdkkonnectcomp.ListAIGatewayModelsResponse{
					Data: []sdkkonnectcomp.AIGatewayModel{
						{
							AIGatewayModelAIGatewayModelModel: &sdkkonnectcomp.AIGatewayModelAIGatewayModelModel{
								ID:   "wrong-variant",
								Name: "llama-3.1",
							},
							Type: sdkkonnectcomp.AIGatewayModelTypeModel,
						},
						{
							AIGatewayModelAIGatewayModelAPI: &sdkkonnectcomp.AIGatewayModelAIGatewayModelAPI{
								ID:   "matched-by-name",
								Name: "llama-3.1",
							},
							Type: sdkkonnectcomp.AIGatewayModelTypeAPI,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayModelForUID(ctx, sdk, model)
		require.NoError(t, err)
		assert.Equal(t, "matched-by-name", id)
	})

	t.Run("returns not found when no matching entry exists", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockAIGatewayModelsSDK(t)
		model := testAIGatewayModel()

		sdk.EXPECT().
			ListAiGatewayModels(mock.Anything, sdkkonnectops.ListAiGatewayModelsRequest{
				GatewayID: "gateway-1",
			}).
			Return(&sdkkonnectops.ListAiGatewayModelsResponse{
				ListAIGatewayModelsResponse: &sdkkonnectcomp.ListAIGatewayModelsResponse{
					Data: []sdkkonnectcomp.AIGatewayModel{
						{
							AIGatewayModelAIGatewayModelAPI: &sdkkonnectcomp.AIGatewayModelAIGatewayModelAPI{
								ID:   "other-id",
								Name: "other-model",
							},
							Type: sdkkonnectcomp.AIGatewayModelTypeAPI,
						},
					},
				},
			}, nil).
			Once()

		id, err := getAIGatewayModelForUID(ctx, sdk, model)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})
}

func testAIGatewayModel() *konnectv1alpha1.AIGatewayModel {
	return &konnectv1alpha1.AIGatewayModel{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayModel",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "aigatewaymodel",
			Namespace:  "default",
			UID:        "aigatewaymodel-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "ai-gw-cp-1",
				},
			},
			APISpec: konnectv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &konnectv1alpha1.AIGatewayModelConfig{
					Type: konnectv1alpha1.AIGatewayModelConfigTypeAPI,
					API: &konnectv1alpha1.AIGatewayModelAPI{
						Name: "llama-3.1",
					},
				},
			},
		},
		Status: konnectv1alpha1.AIGatewayModelStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
		},
	}
}
