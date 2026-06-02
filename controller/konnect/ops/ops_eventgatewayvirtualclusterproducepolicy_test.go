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
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestCreateEventGatewayVirtualClusterProducePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterProducePolicy()

	expectedRequest, err := policy.Spec.APISpec.ToCreateEventGatewayVirtualClusterProducePolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.VirtualClusterID = "virtual-cluster-1"

	sdk.EXPECT().
		CreateEventGatewayVirtualClusterProducePolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyResponse{
			EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{
				ID: "produce-policy-1",
			},
		}, nil).
		Once()

	require.NoError(t, createEventGatewayVirtualClusterProducePolicy(ctx, sdk, policy))
	assert.Equal(t, "produce-policy-1", policy.GetKonnectID())
}

func TestUpdateEventGatewayVirtualClusterProducePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterProducePolicy()
	policy.SetKonnectID("produce-policy-1")

	expectedRequest, err := policy.Spec.APISpec.ToUpdateEventGatewayVirtualClusterProducePolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.VirtualClusterID = "virtual-cluster-1"
	expectedRequest.PolicyID = "produce-policy-1"

	sdk.EXPECT().
		UpdateEventGatewayVirtualClusterProducePolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayVirtualClusterProducePolicy(ctx, sdk, policy))
}

func TestDeleteEventGatewayVirtualClusterProducePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterProducePolicy()
	policy.SetKonnectID("produce-policy-1")

	sdk.EXPECT().
		DeleteEventGatewayVirtualClusterProducePolicy(mock.Anything, sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyRequest{
			GatewayID:        "gateway-1",
			VirtualClusterID: "virtual-cluster-1",
			PolicyID:         "produce-policy-1",
		}).
		Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil).
		Once()

	require.NoError(t, deleteEventGatewayVirtualClusterProducePolicy(ctx, sdk, policy))
}

func TestGetEventGatewayVirtualClusterProducePolicyForUID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterProducePolicy()

	id, err := getEventGatewayVirtualClusterProducePolicyForUID(ctx, sdk, policy)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testEventGatewayVirtualClusterProducePolicy() *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayVirtualClusterProducePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "produce-policy",
			Namespace:  "default",
			UID:        "produce-policy-uid",
			Generation: 2,
		},
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-virtual-cluster",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
				EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
					Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{
						Name:        "add-header-1",
						Description: "produce policy description",
						Labels: configurationv1alpha1.Labels{
							"team": "platform",
						},
						Config: configurationv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
							Actions: []configurationv1alpha1.EventGatewayModifyHeaderAction{
								{
									Op: configurationv1alpha1.EventGatewayModifyHeaderActionTypeSet,
									Set: &configurationv1alpha1.EventGatewayModifyHeaderSetAction{
										Key:   "x-added-header",
										Value: "added-value",
									},
								},
							},
						},
					},
				},
			},
		},
		Status: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			VirtualClusterID: &configurationv1alpha1.KonnectEntityRef{
				ID: "virtual-cluster-1",
			},
		},
	}
}
