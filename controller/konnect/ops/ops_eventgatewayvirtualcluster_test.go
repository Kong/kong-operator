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

func TestCreateEventGatewayVirtualCluster(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
	virtualCluster := testEventGatewayVirtualCluster()

	expectedRequest, err := virtualCluster.Spec.APISpec.ToCreateVirtualClusterRequest()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabels(virtualCluster, expectedRequest.Labels)

	sdk.EXPECT().
		CreateEventGatewayVirtualCluster(mock.Anything, "gateway-1", expectedRequest).
		Return(&sdkkonnectops.CreateEventGatewayVirtualClusterResponse{
			VirtualCluster: &sdkkonnectcomp.VirtualCluster{
				ID: "virtual-cluster-1",
			},
		}, nil).
		Once()

	require.NoError(t, createEventGatewayVirtualCluster(ctx, sdk, virtualCluster))
	assert.Equal(t, "virtual-cluster-1", virtualCluster.GetKonnectID())
}

func TestUpdateEventGatewayVirtualCluster(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
	virtualCluster := testEventGatewayVirtualCluster()
	virtualCluster.SetKonnectID("virtual-cluster-1")

	expectedRequest, err := virtualCluster.Spec.APISpec.ToUpdateVirtualClusterRequest()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabels(virtualCluster, expectedRequest.Labels)

	sdk.EXPECT().
		UpdateEventGatewayVirtualCluster(mock.Anything, sdkkonnectops.UpdateEventGatewayVirtualClusterRequest{
			GatewayID:                   "gateway-1",
			VirtualClusterID:            "virtual-cluster-1",
			UpdateVirtualClusterRequest: expectedRequest,
		}).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterResponse{}, nil).
		Once()

	require.NoError(t, updateEventGatewayVirtualCluster(ctx, sdk, virtualCluster))
}

func TestDeleteEventGatewayVirtualCluster(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
	virtualCluster := testEventGatewayVirtualCluster()
	virtualCluster.SetKonnectID("virtual-cluster-1")

	sdk.EXPECT().
		DeleteEventGatewayVirtualCluster(mock.Anything, "gateway-1", "virtual-cluster-1").
		Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterResponse{}, nil).
		Once()

	require.NoError(t, deleteEventGatewayVirtualCluster(ctx, sdk, virtualCluster))
}

func TestGetEventGatewayVirtualClusterForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
	virtualCluster := testEventGatewayVirtualCluster()

	sdk.EXPECT().
		ListEventGatewayVirtualClusters(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClustersRequest{
			GatewayID: "gateway-1",
		}).
		Return(&sdkkonnectops.ListEventGatewayVirtualClustersResponse{
			ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{
				Data: []sdkkonnectcomp.VirtualCluster{
					{
						ID:     "other-virtual-cluster",
						Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"},
					},
					{
						ID:     "virtual-cluster-1",
						Labels: map[string]string{KubernetesUIDLabelKey: string(virtualCluster.GetUID())},
					},
				},
			},
		}, nil).
		Once()

	id, err := getEventGatewayVirtualClusterForUID(ctx, sdk, virtualCluster)
	require.NoError(t, err)
	assert.Equal(t, "virtual-cluster-1", id)
}

func testEventGatewayVirtualCluster() *konnectv1alpha1.EventGatewayVirtualCluster {
	return &konnectv1alpha1.EventGatewayVirtualCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayVirtualCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "event-virtual-cluster",
			Namespace:  "default",
			UID:        "event-virtual-cluster-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "backend-cluster",
				},
			},
			APISpec: konnectv1alpha1.EventGatewayVirtualClusterAPISpec{
				AclMode:     konnectv1alpha1.VirtualClusterACLMode("enforce_on_gateway"),
				Description: "virtual cluster description",
				DNSLabel:    konnectv1alpha1.VirtualClusterDNSLabel("event-vc"),
				Labels: konnectv1alpha1.Labels{
					"team": "platform",
				},
				Name: konnectv1alpha1.VirtualClusterName("event-virtual-cluster"),
			},
		},
		Status: konnectv1alpha1.EventGatewayVirtualClusterStatus{
			GatewayID: &konnectv1alpha1.KonnectEntityRef{
				ID: "gateway-1",
			},
		},
	}
}
