package ops

import (
	"errors"
	"net/http"
	"testing"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
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
	badRequest := &sdkkonnecterrs.BadRequestError{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "entity is still in use by at least one policy"}
	fromYAML := func(t *testing.T, obj client.Object, manifest string) client.Object {
		t.Helper()
		require.NoError(t, yaml.UnmarshalStrict([]byte(manifest), obj))
		return obj
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		return fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build()
	}

	t.Run("schema registry", func(t *testing.T) {
		t.Parallel()

		registry := &configurationv1alpha1.EventGatewaySchemaRegistry{Name: "orders", Namespace: "default"}
		registry.SetGatewayID(gatewayID)
		registry.SetKonnectID("registry-id")
		policies := func(t *testing.T) []client.Object {
			return []client.Object{
				fromYAML(t, &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}, `
metadata: {name: validate, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: validate
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}, valueValidationAction: reject}}
`),
				fromYAML(t, &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}, `
metadata: {name: check, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: check
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {name: orders}, valueValidationAction: skip}}
`),
				fromYAML(t, &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}, `
metadata: {name: other, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: other
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: payments}}, valueValidationAction: reject}}
`),
			}
		}
		expectDelete := func(t *testing.T, err error) *sdkmocks.MockEventGatewaySchemaRegistriesSDK {
			sdk := sdkmocks.NewMockEventGatewaySchemaRegistriesSDK(t)
			var resp *sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse
			if err == nil {
				resp = &sdkkonnectops.DeleteEventGatewaySchemaRegistryResponse{}
			}
			sdk.EXPECT().DeleteEventGatewaySchemaRegistry(mock.Anything, gatewayID, "registry-id").Return(resp, err).Once()
			return sdk
		}

		t.Run("deletes the registry", func(t *testing.T) {
			require.NoError(t, deleteEventGatewaySchemaRegistryGuarded(t.Context(), expectDelete(t, nil), newClient(t, policies(t)...), registry.DeepCopy()))
		})

		t.Run("reports the policies using it when Konnect refuses the deletion", func(t *testing.T) {
			err := deleteEventGatewaySchemaRegistryGuarded(t.Context(), expectDelete(t, badRequest), newClient(t, policies(t)...), registry.DeepCopy())
			inUse, ok := errors.AsType[EventGatewayEntityInUseError](err)
			require.True(t, ok, "expected EventGatewayEntityInUseError, got %v", err)
			assert.Equal(t, []string{
				"EventGatewayVirtualClusterConsumePolicy default/check",
				"EventGatewayVirtualClusterProducePolicy default/validate",
			}, inUse.Users)
			_, blocked := errors.AsType[DeletionBlockedError](err)
			assert.True(t, blocked, "the reconciler reports it as DeletionBlocked")
			assert.Equal(t, "deletion blocked: the schema registry is in use by EventGatewayVirtualClusterConsumePolicy default/check, "+
				"EventGatewayVirtualClusterProducePolicy default/validate; delete them or stop using the schema registry and the deletion will proceed automatically",
				inUse.DeletionBlockedMessage())
			assert.ErrorIs(t, err, badRequest)
		})

		t.Run("returns Konnect's error when no policy object uses it", func(t *testing.T) {
			err := deleteEventGatewaySchemaRegistryGuarded(t.Context(), expectDelete(t, badRequest), newClient(t), registry.DeepCopy())
			_, blocked := errors.AsType[DeletionBlockedError](err)
			assert.False(t, blocked)
			assert.ErrorIs(t, err, badRequest)
		})
	})

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
		forward := fromYAML(t, &configurationv1alpha1.EventGatewayListenerPolicy{}, `
metadata: {name: forward, namespace: default}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {namespacedRef: {name: orders}}}}
`)
		sdk := sdkmocks.NewMockEventGatewayBackendClustersSDK(t)
		sdk.EXPECT().DeleteEventGatewayBackendCluster(mock.Anything, gatewayID, "bc-id").Return(nil, badRequest).Once()
		err := deleteEventGatewayBackendClusterGuarded(t.Context(), sdk, newClient(t, vc, forward), bc.DeepCopy())
		inUse, ok := errors.AsType[EventGatewayEntityInUseError](err)
		require.True(t, ok, "expected EventGatewayEntityInUseError, got %v", err)
		assert.Equal(t, []string{"EventGatewayListenerPolicy default/forward"}, inUse.Users)
		assert.Equal(t, "backend cluster", inUse.What)
	})

	t.Run("virtual cluster", func(t *testing.T) {
		t.Parallel()

		vc := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "orders", Namespace: "default"}
		vc.SetGatewayID(gatewayID)
		vc.SetKonnectID("vc-id")
		forward := fromYAML(t, &configurationv1alpha1.EventGatewayListenerPolicy{}, `
metadata: {name: forward, namespace: default}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: {namespacedRef: {name: orders}}}}
`)
		expectDelete := func(t *testing.T, err error) *sdkmocks.MockEventGatewayVirtualClustersSDK {
			sdk := sdkmocks.NewMockEventGatewayVirtualClustersSDK(t)
			sdk.EXPECT().DeleteEventGatewayVirtualCluster(mock.Anything, gatewayID, "vc-id").Return(nil, err).Once()
			return sdk
		}

		t.Run("reports the listener policies forwarding to it when Konnect refuses the deletion", func(t *testing.T) {
			err := deleteEventGatewayVirtualClusterGuarded(t.Context(), expectDelete(t, badRequest), newClient(t, forward), vc.DeepCopy())
			inUse, ok := errors.AsType[EventGatewayEntityInUseError](err)
			require.True(t, ok, "expected EventGatewayEntityInUseError, got %v", err)
			assert.Equal(t, []string{"EventGatewayListenerPolicy default/forward"}, inUse.Users)
			assert.Equal(t, "virtual cluster", inUse.What)
		})

		t.Run("returns any other error as it is", func(t *testing.T) {
			other := errors.New("unavailable")
			err := deleteEventGatewayVirtualClusterGuarded(t.Context(), expectDelete(t, other), newClient(t, forward), vc.DeepCopy())
			_, blocked := errors.AsType[DeletionBlockedError](err)
			assert.False(t, blocked)
		})
	})
}
