package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueEventGatewayBackendClusterForEventGatewayListenerPolicy(t *testing.T) {
	fromYAML := func(obj client.Object, manifest string) client.Object {
		require.NoError(t, yaml.UnmarshalStrict([]byte(manifest), obj))
		return obj
	}
	orders := fromYAML(&configurationv1alpha1.EventGatewayVirtualCluster{}, `
metadata: {name: orders, namespace: default}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
`)
	forward := func(destination string) *configurationv1alpha1.EventGatewayListenerPolicy {
		return fromYAML(&configurationv1alpha1.EventGatewayListenerPolicy{}, `
metadata: {name: forward, namespace: default}
spec:
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config: {type: portMapping, portMapping: {advertisedHost: localhost, destination: `+destination+`}}
`).(*configurationv1alpha1.EventGatewayListenerPolicy)
	}
	cl := newFakeClientBuilder(scheme.Get()).WithObjects(orders).Build()
	enqueue := enqueueEventGatewayBackendClusterForEventGatewayListenerPolicy(cl)

	assert.Equal(t, []reconcile.Request{{Namespace: "default", Name: "bc"}},
		enqueue(t.Context(), forward(`{namespacedRef: {name: orders}}`)), "the backend cluster of the virtual cluster it forwards to")
	assert.Empty(t, enqueue(t.Context(), forward(`{namespacedRef: {name: missing}}`)), "a virtual cluster that doesn't exist")
	assert.Empty(t, enqueue(t.Context(), forward(`{name: orders}`)), "a destination by Konnect name holds nothing")
}
