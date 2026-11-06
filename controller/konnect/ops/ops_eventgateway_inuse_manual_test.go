package ops

import (
	"errors"
	"net/http"
	"testing"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestDeleteEventGatewayEntitiesInUseGuarded(t *testing.T) {
	t.Parallel()

	const gatewayID = "gateway-1"
	// badRequest returns a new error each time: the deletion clears its
	// Instance, which would race between the parallel subtests.
	badRequest := func() error {
		return &sdkkonnecterrs.BadRequestError{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "entity is still in use by at least one policy"}
	}
	fromYAML := func(t *testing.T, obj client.Object, manifest string) client.Object {
		t.Helper()
		require.NoError(t, yaml.UnmarshalStrict([]byte(manifest), obj))
		return obj
	}
	// inKonnect sets the Konnect ID of a policy object: only those created in
	// Konnect can be what Konnect refuses a deletion for.
	inKonnect := func(obj client.Object) client.Object {
		obj.(interface{ SetKonnectID(string) }).SetKonnectID("konnect-" + obj.GetName()) //nolint:forcetypeassert
		return obj
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build()
	}

	t.Run("backend cluster", func(t *testing.T) {
		t.Parallel()

		bc := &configurationv1alpha1.EventGatewayBackendCluster{Name: "bc", Namespace: "default"}
		bc.SetGatewayID(gatewayID)
		bc.SetKonnectID("bc-id")
		vc := fromYAML(t, &configurationv1alpha1.EventGatewayVirtualCluster{}, `
metadata: {name: orders, namespace: default}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
`)
		forward := inKonnect(fromYAML(t, &configurationv1alpha1.EventGatewayListenerPolicy{}, `
metadata: {name: forward, namespace: default}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {namespacedRef: {name: orders}}}}
`))
		sdk := sdkmocks.NewMockEventGatewayBackendClustersSDK(t)
		sdk.EXPECT().DeleteEventGatewayBackendCluster(mock.Anything, gatewayID, "bc-id").Return(nil, badRequest()).Once()
		err := deleteEventGatewayBackendClusterGuarded(t.Context(), sdk, newClient(t, vc, forward), bc.DeepCopy())
		inUse, ok := errors.AsType[EventGatewayEntityInUseError](err)
		require.True(t, ok, "expected EventGatewayEntityInUseError, got %v", err)
		assert.Equal(t, []string{"EventGatewayListenerPolicy default/forward"}, inUse.Users)
		assert.Equal(t, "backend cluster", inUse.What)
		assert.Equal(t, "deletion blocked: the backend cluster is in use by EventGatewayListenerPolicy default/forward; "+
			"delete them or stop forwarding to its virtual clusters and the deletion will proceed automatically", inUse.DeletionBlockedMessage())
	})

	t.Run("virtual cluster", func(t *testing.T) {
		t.Parallel()

		vc := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "orders", Namespace: "default"}
		vc.SetGatewayID(gatewayID)
		vc.SetKonnectID("vc-id")
		forward := inKonnect(fromYAML(t, &configurationv1alpha1.EventGatewayListenerPolicy{}, `
metadata: {name: forward, namespace: default}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {namespacedRef: {name: orders}}}}
`))
		expectDelete := func(t *testing.T, err error) *sdkmocks.MockEventGatewayVirtualClustersSDK {
			sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
			sdk.EXPECT().DeleteEventGatewayVirtualCluster(mock.Anything, gatewayID, "vc-id").Return(nil, err).Once()
			return sdk
		}

		t.Run("reports the listener policies forwarding to it when Konnect refuses the deletion", func(t *testing.T) {
			err := deleteEventGatewayVirtualClusterGuarded(t.Context(), expectDelete(t, badRequest()), newClient(t, forward), vc.DeepCopy())
			inUse, ok := errors.AsType[EventGatewayEntityInUseError](err)
			require.True(t, ok, "expected EventGatewayEntityInUseError, got %v", err)
			assert.Equal(t, []string{"EventGatewayListenerPolicy default/forward"}, inUse.Users)
			assert.Equal(t, "virtual cluster", inUse.What)
		})

		t.Run("returns Konnect's error when the listener policy isn't in Konnect", func(t *testing.T) {
			pending := forward.DeepCopyObject().(*configurationv1alpha1.EventGatewayListenerPolicy)
			pending.SetKonnectID("")
			refused := badRequest()
			err := deleteEventGatewayVirtualClusterGuarded(t.Context(), expectDelete(t, refused), newClient(t, pending), vc.DeepCopy())
			_, blocked := errors.AsType[DeletionBlockedError](err)
			assert.False(t, blocked)
			assert.ErrorIs(t, err, refused)
		})

		t.Run("returns any other error as it is", func(t *testing.T) {
			other := errors.New("unavailable")
			err := deleteEventGatewayVirtualClusterGuarded(t.Context(), expectDelete(t, other), newClient(t, forward), vc.DeepCopy())
			_, blocked := errors.AsType[DeletionBlockedError](err)
			assert.False(t, blocked)
		})
	})
}
