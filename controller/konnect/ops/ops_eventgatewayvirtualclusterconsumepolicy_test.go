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

func TestCreateEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterConsumePolicy()

	expectedRequest, err := policy.Spec.APISpec.ToCreateEventGatewayVirtualClusterConsumePolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.VirtualClusterID = "virtual-cluster-1"

	sdk.EXPECT().
		CreateEventGatewayVirtualClusterConsumePolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyResponse{
			EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{
				ID: "consume-policy-1",
			},
		}, nil).
		Once()

	require.NoError(t, createEventGatewayVirtualClusterConsumePolicy(ctx, sdk, policy))
	assert.Equal(t, "consume-policy-1", policy.GetKonnectID())
}

func TestUpdateEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterConsumePolicy()
	policy.SetKonnectID("consume-policy-1")

	expectedRequest, err := policy.Spec.APISpec.ToUpdateEventGatewayVirtualClusterConsumePolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.VirtualClusterID = "virtual-cluster-1"
	expectedRequest.PolicyID = "consume-policy-1"

	sdk.EXPECT().
		UpdateEventGatewayVirtualClusterConsumePolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayVirtualClusterConsumePolicy(ctx, sdk, policy))
}

func TestDeleteEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterConsumePolicy()
	policy.SetKonnectID("consume-policy-1")

	sdk.EXPECT().
		DeleteEventGatewayVirtualClusterConsumePolicy(mock.Anything, sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyRequest{
			GatewayID:        "gateway-1",
			VirtualClusterID: "virtual-cluster-1",
			PolicyID:         "consume-policy-1",
		}).
		Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil).
		Once()

	require.NoError(t, deleteEventGatewayVirtualClusterConsumePolicy(ctx, sdk, policy))
}

func TestGetEventGatewayVirtualClusterConsumePolicyForUID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	policy := testEventGatewayVirtualClusterConsumePolicy()

	id, err := getEventGatewayVirtualClusterConsumePolicyForUID(ctx, sdk, policy)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testEventGatewayVirtualClusterConsumePolicy() *konnectv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	return &konnectv1alpha1.EventGatewayVirtualClusterConsumePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayVirtualClusterConsumePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "consume-policy",
			Namespace:  "default",
			UID:        "consume-policy-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-virtual-cluster",
				},
			},
			APISpec: konnectv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &konnectv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
					Type: konnectv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &konnectv1alpha1.EventGatewayModifyHeadersPolicyCreate{
						Name:        "add-header-1",
						Description: "consume policy description",
						Labels: konnectv1alpha1.Labels{
							"team": "platform",
						},
						Config: konnectv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
							Actions: []konnectv1alpha1.EventGatewayModifyHeaderAction{
								{
									Op: konnectv1alpha1.EventGatewayModifyHeaderActionTypeSet,
									Set: &konnectv1alpha1.EventGatewayModifyHeaderSetAction{
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
		Status: konnectv1alpha1.EventGatewayVirtualClusterConsumePolicyStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			VirtualClusterID: &konnectv1alpha1.KonnectEntityRef{
				ID: "virtual-cluster-1",
			},
		},
	}
}
