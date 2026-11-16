package onprem

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

// producePolicyTreeObjects returns a gateway with a virtual cluster whose
// produce policies are a root schemaValidation policy with two children and a
// separate branch: two root groups. The policies are created as parse, first,
// second, then root: a name sort of the first three would put a child before
// its parent.
func producePolicyTreeObjects(t *testing.T) (
	*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
	*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
	*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
	*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy,
	[]client.Object,
) {
	t.Helper()
	created := time.Date(2026, 11, 16, 9, 48, 16, 0, time.UTC)
	gateway := newTestGateway("gateway")
	backend := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: backend}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: backend
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
	vc := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vcluster}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vcluster
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vcluster
`)
	parse := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created, `
metadata: {name: parse}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: parse
      config:
        type: json
        json:
          valueValidationAction: reject
`)
	first := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created, `
metadata: {name: first}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: modifyHeaders
    parentPolicyRef: {name: parse}
    modifyHeaders:
      name: first
      config:
        actions:
        - op: set
          set: {key: x-produce-stage, value: first}
`)
	second := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created, `
metadata: {name: second}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: modifyHeaders
    parentPolicyRef: {name: parse}
    modifyHeaders:
      name: second
      config:
        actions:
        - op: set
          set: {key: x-produce-stage, value: second}
`)
	root := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created.Add(time.Second), `
metadata: {name: root}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: root
      config:
        actions:
        - op: set
          set: {key: x-produce-root, value: example}
`)
	return parse, first, second, root, []client.Object{gateway, backend, vc, parse, first, second, root}
}

// TestBuildProducePolicyDefaultOrderKeepsGroupsContiguous checks that without a
// chain, the produce policies order keeps each parent and its children
// contiguous: gw-admin rejects a root inserted between a parent and its
// children.
func TestBuildProducePolicyDefaultOrderKeepsGroupsContiguous(t *testing.T) {
	parse, first, second, root, objects := producePolicyTreeObjects(t)
	gateway := objects[0].(*eventgatewayv1alpha1.OnPremEventGateway)
	cl := newFakeClient(t, objects...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.invalidChains)

	names := map[string]string{
		string(parse.UID):  "parse",
		string(first.UID):  "first",
		string(second.UID): "second",
		string(root.UID):   "root",
	}
	var got []string
	for _, p := range cfg.entities.VirtualClusters[0].ProducePolicies {
		got = append(got, names[p.Id.String()])
	}
	require.Equal(t, []string{"parse", "first", "second", "root"}, got)
	require.NoError(t, configurationv1alpha1.ValidateProducePolicyChainOrder(got, map[string]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
		"parse": parse, "first": first, "second": second, "root": root,
	}))
}

// TestReconcileProducePolicyChainLifecycle checks that a chain order is
// pushed, an incomplete chain freezes the gateway, and deleting the chain
// restores the default order, which then requires no further push.
func TestReconcileProducePolicyChainLifecycle(t *testing.T) {
	parse, first, second, root, objects := producePolicyTreeObjects(t)
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	for _, obj := range objects[1:] {
		require.NoError(t, f.cl.Create(t.Context(), obj))
	}
	chain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, time.Now().UTC(), `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    policies:
    - {name: root}
    - {name: parse}
    - {name: first}
    - {name: second}
`)
	require.NoError(t, f.cl.Create(t.Context(), chain))

	names := map[string]string{
		string(parse.UID):  "parse",
		string(first.UID):  "first",
		string(second.UID): "second",
		string(root.UID):   "root",
	}
	reconcile := func() {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		f.pod().mu.Lock()
		if held, ok := f.pod().gateways[f.gatewayID]; ok {
			f.pod().gateways[f.gatewayID] = withGWAdminDefaults(held)
		}
		f.pod().mu.Unlock()
	}
	heldNames := func() []string {
		t.Helper()
		pod := f.pod()
		pod.mu.Lock()
		defer pod.mu.Unlock()
		held := pod.gateways[f.gatewayID].VirtualClusters
		require.Len(t, held, 1)
		namesOut := make([]string, 0, len(held[0].ProducePolicies))
		for _, p := range held[0].ProducePolicies {
			namesOut = append(namesOut, names[p.Id.String()])
		}
		return namesOut
	}

	reconcile()
	require.Equal(t, []string{"root", "parse", "first", "second"}, heldNames())

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[1:3]
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "an incomplete owner freezes the whole document")
	require.Equal(t, []string{"root", "parse", "first", "second"}, heldNames())

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, chain)))
	f.pod().reset()
	reconcile()
	require.Contains(t, f.pod().recorded(), callEntities, "deleting the chain restores the default order")
	require.Equal(t, []string{"parse", "first", "second", "root"}, heldNames())

	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "a no-chain configuration is not pushed repeatedly")
}
