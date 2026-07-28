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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestCreateEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()
	policy := testEventGatewayVirtualClusterConsumePolicy()

	expectedRequest, err := policy.ToCreateEventGatewayVirtualClusterConsumePolicyRequest(ctx, cl)
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

	require.NoError(t, createEventGatewayVirtualClusterConsumePolicy(ctx, cl, sdk, policy))
	assert.Equal(t, "consume-policy-1", policy.GetKonnectID())
}

func TestUpdateEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()
	policy := testEventGatewayVirtualClusterConsumePolicy()
	policy.SetKonnectID("consume-policy-1")

	expectedRequest, err := policy.ToUpdateEventGatewayVirtualClusterConsumePolicyRequest(ctx, cl)
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.VirtualClusterID = "virtual-cluster-1"
	expectedRequest.PolicyID = "consume-policy-1"

	sdk.EXPECT().
		UpdateEventGatewayVirtualClusterConsumePolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayVirtualClusterConsumePolicy(ctx, cl, sdk, policy))
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

func testEventGatewayVirtualClusterConsumePolicy() *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	return &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayVirtualClusterConsumePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "consume-policy",
			Namespace:  "default",
			UID:        "consume-policy-uid",
			Generation: 2,
		},
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-virtual-cluster",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
					Type: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{
						Name:        "add-header-1",
						Description: "consume policy description",
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
		Status: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			VirtualClusterID: &configurationv1alpha1.KonnectEntityRef{
				ID: "virtual-cluster-1",
			},
		},
	}
}
