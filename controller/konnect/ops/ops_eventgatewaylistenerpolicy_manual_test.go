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

	require.NoError(t, createEventGatewayListenerPolicy(ctx, nil, sdk, policy))
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

	require.NoError(t, updateEventGatewayListenerPolicy(ctx, nil, sdk, policy))
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

func testEventGatewayListenerPolicy() *configurationv1alpha1.EventGatewayListenerPolicy {
	return &configurationv1alpha1.EventGatewayListenerPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayListenerPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "listener-policy",
			Namespace:  "default",
			UID:        "listener-policy-uid",
			Generation: 2,
		},
		Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-gateway-listener",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayListenerPolicyAPISpec{
				EventGatewayListenerPolicyConfig: &configurationv1alpha1.EventGatewayListenerPolicyConfig{
					Type: configurationv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
					EventGatewayTLSListen: &configurationv1alpha1.EventGatewayTLSListenerPolicy{
						Name:        "tls-policy",
						Description: "listener tls policy",
						Config: configurationv1alpha1.EventGatewayTLSListenerPolicyConfig{
							Certificates: []configurationv1alpha1.TLSCertificate{
								{
									Certificate: configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----test-----END CERTIFICATE-----")},
									Key:         configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("-----BEGIN PRIVATE KEY-----test-----END PRIVATE KEY-----")},
								},
							},
							ClientAuthentication: configurationv1alpha1.EventGatewayTLSListenerPolicyConfigClientAuthentication{
								Mode: "requested",
								TLSTrustBundles: []configurationv1alpha1.TLSTrustBundleReference{
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
		Status: configurationv1alpha1.EventGatewayListenerPolicyStatus{
			GatewayID: &configurationv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
			EventGatewayListenerID: &configurationv1alpha1.KonnectEntityRef{
				ID: "listener-1",
			},
		},
	}
}
