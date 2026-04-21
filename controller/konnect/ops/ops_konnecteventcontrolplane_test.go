package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestCreateKonnectEventControlPlane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockEventGatewaysSDK(t)
	cp := testKonnectEventControlPlane()

	expectedRequest, err := cp.Spec.APISpec.ToCreateGatewayRequest()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)

	sdk.EXPECT().
		CreateEventGateway(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayResponse{
			EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
				ID: "gateway-1",
			},
		}, nil).
		Once()

	err = createKonnectEventControlPlane(ctx, sdk, cp)
	require.NoError(t, err)
	assert.Equal(t, "gateway-1", cp.GetKonnectID())
}

func TestUpdateKonnectEventControlPlane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockEventGatewaysSDK(t)
	cp := testKonnectEventControlPlane()
	cp.SetKonnectID("gateway-1")

	expectedRequest, err := cp.Spec.APISpec.ToUpdateGatewayRequest()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)

	sdk.EXPECT().
		UpdateEventGateway(mock.Anything, "gateway-1", *expectedRequest).
		Return(&sdkkonnectops.UpdateEventGatewayResponse{
			EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
				ID: "gateway-1",
			},
		}, nil).
		Once()

	err = updateKonnectEventControlPlane(ctx, sdk, cp)
	require.NoError(t, err)
	assert.Equal(t, "gateway-1", cp.GetKonnectID())
}

func TestDeleteKonnectEventControlPlane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockEventGatewaysSDK(t)
	cp := testKonnectEventControlPlane()
	cp.SetKonnectID("gateway-1")

	sdk.EXPECT().
		DeleteEventGateway(mock.Anything, "gateway-1").
		Return(&sdkkonnectops.DeleteEventGatewayResponse{}, nil).
		Once()

	err := deleteKonnectEventControlPlane(ctx, sdk, cp)
	require.NoError(t, err)
}

func TestGetKonnectEventControlPlaneForUID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockEventGatewaysSDK(t)
	cp := testKonnectEventControlPlane()

	sdk.EXPECT().
		ListEventGateways(mock.Anything, sdkkonnectops.ListEventGatewaysRequest{
			// TODO: this will be more specific when we start generating getForUID functions
			// with support for filters derived from the OpenAPI schema.
		}).
		Return(&sdkkonnectops.ListEventGatewaysResponse{
			ListEventGatewaysResponse: &sdkkonnectcomp.ListEventGatewaysResponse{
				Data: []sdkkonnectcomp.EventGatewayInfo{
					{
						ID:     "gateway-other",
						Name:   "event-control-plane",
						Labels: map[string]string{KubernetesUIDLabelKey: "different-uid"},
					},
					{
						ID:     "gateway-1",
						Name:   "event-control-plane",
						Labels: map[string]string{KubernetesUIDLabelKey: string(cp.GetUID())},
					},
				},
			},
		}, nil).
		Once()

	id, err := getKonnectEventControlPlaneForUID(ctx, sdk, cp)
	require.NoError(t, err)
	assert.Equal(t, "gateway-1", id)
}

func testKonnectEventControlPlane() *konnectv1alpha1.KonnectEventControlPlane {
	return &konnectv1alpha1.KonnectEventControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectEventControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "event-control-plane",
			Namespace:  "default",
			UID:        "event-control-plane-uid",
			Generation: 3,
		},
		Spec: konnectv1alpha1.KonnectEventControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			APISpec: konnectv1alpha1.KonnectEventControlPlaneAPISpec{
				Name:              "event-control-plane",
				Description:       "Event gateway description",
				MinRuntimeVersion: "3.8",
				Labels: konnectv1alpha1.Labels{
					"team": "platform",
				},
			},
		},
	}
}
