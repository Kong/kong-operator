package ops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestListenerPolicyVirtualClusterReferences(t *testing.T) {
	t.Parallel()

	virtualCluster := func(name, gatewayID, konnectID string) *configurationv1alpha1.EventGatewayVirtualCluster {
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: `+name+`, namespace: default}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: `+name+`-in-konnect
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: `+name+`
`), &vc))
		vc.SetGatewayID(gatewayID)
		vc.SetKonnectID(konnectID)
		return &vc
	}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(
		virtualCluster("orders", "gateway-1", "orders-id"),
		virtualCluster("elsewhere", "gateway-2", "elsewhere-id"),
		virtualCluster("not-programmed", "gateway-1", ""),
		func() *configurationv1alpha1.EventGatewayVirtualCluster {
			vc := virtualCluster("being-deleted", "gateway-1", "being-deleted-id")
			vc.SetFinalizers([]string{"example.com/hold"})
			vc.SetDeletionTimestamp(new(metav1.Now()))
			return vc
		}(),
	).Build()

	policy := func(t *testing.T, destination string) *configurationv1alpha1.EventGatewayListenerPolicy {
		t.Helper()
		var p configurationv1alpha1.EventGatewayListenerPolicy
		require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: forward, namespace: default}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: forward
      config:
        type: portMapping
        portMapping: {advertisedHost: localhost, destination: `+destination+`}
`), &p))
		p.SetGatewayID("gateway-1")
		return &p
	}
	destinationSent := func(t *testing.T, p *configurationv1alpha1.EventGatewayListenerPolicy) any {
		t.Helper()
		req, err := p.ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		data, err := json.Marshal(req.EventGatewayListenerPolicyCreate)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(data, &body))
		return body["config"].(map[string]any)["destination"]
	}

	t.Run("a reference by object is sent by its Konnect ID", func(t *testing.T) {
		t.Parallel()

		p := policy(t, `{namespacedRef: {name: orders}}`)
		assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "orders"}},
			configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayVirtualCluster(p))
		assert.Equal(t, map[string]any{"id": "orders-id"}, destinationSent(t, p))
	})

	t.Run("references by id and by name are sent as they are", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, map[string]any{"id": "konnect-id"}, destinationSent(t, policy(t, `{id: konnect-id}`)))
		assert.Equal(t, map[string]any{"name": "konnect-name"}, destinationSent(t, policy(t, `{name: konnect-name}`)))
	})

	t.Run("a virtual cluster that doesn't exist is reported", func(t *testing.T) {
		t.Parallel()

		_, err := policy(t, `{namespacedRef: {name: missing}}`).ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
		var notFound configurationv1alpha1.ReferenceNotFoundError
		require.ErrorAs(t, err, &notFound)
	})

	t.Run("a virtual cluster of another gateway is reported", func(t *testing.T) {
		t.Parallel()

		_, err := policy(t, `{namespacedRef: {name: elsewhere}}`).ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
		var otherGateway configurationv1alpha1.ReferenceDifferentGatewayError
		require.ErrorAs(t, err, &otherGateway)
	})

	// Konnect refuses to delete a virtual cluster in use: a new policy could keep
	// the deletion waiting.
	t.Run("a virtual cluster being deleted can't get a new policy, but keeps an existing one", func(t *testing.T) {
		t.Parallel()

		_, err := policy(t, `{namespacedRef: {name: being-deleted}}`).ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
		var beingDeleted configurationv1alpha1.ReferenceBeingDeletedError
		require.ErrorAs(t, err, &beingDeleted)

		existing := policy(t, `{namespacedRef: {name: being-deleted}}`)
		existing.SetKonnectID("policy-id")
		assert.Equal(t, map[string]any{"id": "being-deleted-id"}, destinationSent(t, existing))
	})

	t.Run("a virtual cluster not in Konnect yet is reported", func(t *testing.T) {
		t.Parallel()

		_, err := policy(t, `{namespacedRef: {name: not-programmed}}`).ToCreateEventGatewayListenerPolicyRequest(t.Context(), cl)
		var notProgrammed configurationv1alpha1.ReferenceNotProgrammedError
		require.ErrorAs(t, err, &notProgrammed)
	})
}
