package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateAndUpdateKongRoute_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx   = context.Background()
		route = &configurationv1alpha1.KongRoute{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongRoute",
				APIVersion: "configuration.konghq.com/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "route-1",
				Namespace: "default",
				UID:       k8stypes.UID(uuid.NewString()),
			},
			Spec: configurationv1alpha1.KongRouteSpec{
				ServiceRef: &configurationv1alpha1.ServiceRef{
					Type: configurationv1alpha1.ServiceRefNamespacedRef,
					NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
						Name: "service-1",
					},
				},
			},
			Status: configurationv1alpha1.KongRouteStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{
					ControlPlaneID: "12345",
				},
			},
		}
		sdk = &MockRoutesSDK{}
	)

	t.Log("Triggering CreateRoute and capturing generated tags")
	sdk.EXPECT().
		CreateRoute(ctx, route.GetControlPlaneID(), mock.Anything).
		Return(&sdkkonnectops.CreateRouteResponse{
			Route: &sdkkonnectcomp.Route{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err := createRoute(ctx, sdk, route)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 1)
	call := sdk.Calls[0]
	require.Equal(t, "CreateRoute", call.Method)
	require.IsType(t, sdkkonnectcomp.RouteInput{}, call.Arguments[2])
	capturedCreateTags := call.Arguments[2].(sdkkonnectcomp.RouteInput).Tags

	t.Log("Triggering UpsertRoute and capturing generated tags")
	sdk.EXPECT().
		UpsertRoute(ctx, mock.Anything).
		Return(&sdkkonnectops.UpsertRouteResponse{
			Route: &sdkkonnectcomp.Route{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	err = updateRoute(ctx, sdk, route)
	require.NoError(t, err)
	require.Len(t, sdk.Calls, 2)
	call = sdk.Calls[1]
	require.Equal(t, "UpsertRoute", call.Method)
	require.IsType(t, sdkkonnectops.UpsertRouteRequest{}, call.Arguments[1])
	capturedUpdateTags := call.Arguments[1].(sdkkonnectops.UpsertRouteRequest).Route.Tags

	require.Equal(t, capturedCreateTags, capturedUpdateTags, "tags should be consistent between create and update")
}
