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

func TestCreateEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()

	expectedRequest, err := policy.Spec.APISpec.ToCreateEventGatewayListenerPolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.ListenerID = "listener-1"

	sdk.EXPECT().
		CreateEventGatewayListenerPolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayListenerPolicyResponse{
			EventGatewayListenerPolicy: &sdkkonnectcomp.EventGatewayListenerPolicy{
				ID: "listener-policy-1",
			},
		}, nil).
		Once()

	require.NoError(t, createEventGatewayListenerPolicy(ctx, sdk, policy))
	assert.Equal(t, "listener-policy-1", policy.GetKonnectID())
}

func TestUpdateEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()
	policy.SetKonnectID("listener-policy-1")

	expectedRequest, err := policy.Spec.APISpec.ToUpdateEventGatewayListenerPolicyRequest()
	require.NoError(t, err)
	expectedRequest.GatewayID = "gateway-1"
	expectedRequest.ListenerID = "listener-1"
	expectedRequest.PolicyID = "listener-policy-1"

	sdk.EXPECT().
		UpdateEventGatewayListenerPolicy(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayListenerPolicyResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayListenerPolicy(ctx, sdk, policy))
}

func TestDeleteEventGatewayListenerPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()
	policy.SetKonnectID("listener-policy-1")

	sdk.EXPECT().
		DeleteEventGatewayListenerPolicy(mock.Anything, sdkkonnectops.DeleteEventGatewayListenerPolicyRequest{
			GatewayID:  "gateway-1",
			ListenerID: "listener-1",
			PolicyID:   "listener-policy-1",
		}).
		Return(&sdkkonnectops.DeleteEventGatewayListenerPolicyResponse{}, nil).
		Once()

	require.NoError(t, deleteEventGatewayListenerPolicy(ctx, sdk, policy))
}

func TestGetEventGatewayListenerPolicyForUID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayListenerPoliciesSDK(t)
	policy := testEventGatewayListenerPolicy()

	sdk.EXPECT().
		ListEventGatewayListenerPolicies(mock.Anything, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
			GatewayID:  "gateway-1",
			ListenerID: "listener-1",
		}).
		Return(&sdkkonnectops.ListEventGatewayListenerPoliciesResponse{
			ListEventGatewayListenerPoliciesResponse: []sdkkonnectcomp.EventGatewayListenerPolicy{
				{
					ID:   "other-policy",
					Type: "forward_to_virtual_cluster",
					Name: new("tls-policy"),
				},
				{
					ID:   "listener-policy-1",
					Type: "tls_server",
					Name: new("tls-policy"),
				},
			},
		}, nil).
		Once()

	id, err := getEventGatewayListenerPolicyForUID(ctx, sdk, policy)
	require.NoError(t, err)
	assert.Equal(t, "listener-policy-1", id)
}

func testEventGatewayListenerPolicy() *konnectv1alpha1.EventGatewayListenerPolicy {
	return &konnectv1alpha1.EventGatewayListenerPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayListenerPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "listener-policy",
			Namespace:  "default",
			UID:        "listener-policy-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-gateway-listener",
				},
			},
			APISpec: konnectv1alpha1.EventGatewayListenerPolicyAPISpec{
				EventGatewayListenerPolicyConfig: &konnectv1alpha1.EventGatewayListenerPolicyConfig{
					Type: konnectv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
					EventGatewayTLSListen: &konnectv1alpha1.EventGatewayTLSListenerPolicy{
						Name:        "tls-policy",
						Description: "listener tls policy",
						Config: konnectv1alpha1.EventGatewayTLSListenerPolicyConfig{
							Certificates: []konnectv1alpha1.TLSCertificate{
								{
									Certificate: "-----BEGIN CERTIFICATE-----test-----END CERTIFICATE-----",
									Key:         "-----BEGIN PRIVATE KEY-----test-----END PRIVATE KEY-----",
								},
							},
							ClientAuthentication: konnectv1alpha1.ClientAuthentication{
								Mode: "requested",
								TLSTrustBundles: []konnectv1alpha1.TLSTrustBundleReference{
									{
										ID: new("trust-bundle-1"),
									},
								},
							},
						},
					},
				},
			},
		},
		Status: konnectv1alpha1.EventGatewayListenerPolicyStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			EventGatewayListenerID: &konnectv1alpha1.KonnectEntityRef{
				ID: "listener-1",
			},
		},
	}
}
