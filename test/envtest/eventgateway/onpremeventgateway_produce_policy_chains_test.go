package eventgateway

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestOnPremEventGatewayProducePolicyChain checks that a produce chain orders
// the complete produce list of the gw-admin document, independently of the
// cluster-level list, and that an invalid declaration blocks the push instead
// of applying a shortened chain.
func TestOnPremEventGatewayProducePolicyChain(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))
	pod := gwAdmin.provision(ctx)
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	backend := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, backendClusterYAML("backend", "backend", gateway.Name))
	parent := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	chain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, ns.Name, `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies:
    - {name: parent}
    - {name: second}
    - {name: first}
    - {name: root}
`)
	for _, obj := range []client.Object{gateway, backend, parent, chain} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	expectCondition := func(obj interface {
		client.Object
		GetConditions() []metav1.Condition
	}, typ string, status metav1.ConditionStatus, reason string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
				return
			}
			cond := apimeta.FindStatusCondition(obj.GetConditions(), typ)
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, status, cond.Status, cond.Message)
				assert.Equal(ct, reason, cond.Reason, cond.Message)
				assert.Equal(ct, obj.GetGeneration(), cond.ObservedGeneration)
			}
		}, waitTime, tickTime)
	}
	expectOrder := func(ids ...string) int {
		t.Helper()
		version := 0
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
				return
			}
			got := make([]string, 0, len(g.entities.VirtualClusters[0].ProducePolicies))
			for _, p := range g.entities.VirtualClusters[0].ProducePolicies {
				got = append(got, p.Id.String())
			}
			assert.Equal(ct, ids, got)
			version = g.version.DataVersion
		}, waitTime, tickTime)
		return version
	}
	policy := func(name, manifest string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, ns.Name, fmt.Sprintf(`
metadata: {name: %s}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
%s
`, name, manifest))
	}
	// The parent and the root are modified headers: neither needs a parent
	// policy, and the two children are siblings of the parent group.
	parentPolicy := policy("parent", `
    type: schemaValidation
    schemaValidation:
      name: parent
      config:
        type: json
        json:
          valueValidationAction: reject`)
	first := policy("first", `
    type: modifyHeaders
    parentPolicyRef: {name: parent}
    modifyHeaders:
      name: first
      config:
        actions: [{op: remove, remove: {key: x-first}}]`)
	second := policy("second", `
    type: modifyHeaders
    parentPolicyRef: {name: parent}
    modifyHeaders:
      name: second
      config:
        actions: [{op: remove, remove: {key: x-second}}]`)
	root := policy("root", `
    type: modifyHeaders
    modifyHeaders:
      name: root
      config:
        actions: [{op: remove, remove: {key: x-root}}]`)

	// The chain names policies that don't exist yet: it is invalid, and the
	// gateway keeps pushing the document without them.
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	for _, obj := range []client.Object{root, second, parentPolicy, first} {
		require.NoError(t, cl.Create(ctx, obj), "creating %T %s (resourceVersion %q)", obj, client.ObjectKeyFromObject(obj), obj.GetResourceVersion())
	}
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	version := expectOrder(string(parentPolicy.UID), string(second.UID), string(first.UID), string(root.UID))
	require.Empty(t, chain.GetKonnectID(), "the chain has no independent ID")

	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		// Siblings may reorder; the root group stays contiguous.
		chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{
			{Name: "root"}, {Name: "parent"}, {Name: "first"}, {Name: "second"},
		}
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Greater(t, expectOrder(string(root.UID), string(parentPolicy.UID), string(first.UID), string(second.UID)), version)

	// A child before its parent can't be applied: the running document stays
	// as it was, and the chain reports why.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{
			{Name: "first"}, {Name: "parent"}, {Name: "second"}, {Name: "root"},
		}
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	require.Contains(t, apimeta.FindStatusCondition(chain.GetConditions(), "Programmed").Message, "contiguous group")
	expectOrder(string(root.UID), string(parentPolicy.UID), string(first.UID), string(second.UID))

	// A competing produce chain can't claim the virtual cluster, and leaves
	// the declared order of the owner alone.
	competitor := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, ns.Name, `
metadata: {name: competitor}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: parent}, {name: second}, {name: first}, {name: root}]
`)
	require.NoError(t, cl.Create(ctx, competitor))
	expectCondition(competitor, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	expectCondition(competitor, "Programmed", metav1.ConditionFalse, "Invalid")
	require.NoError(t, cl.Delete(ctx, competitor))

	// A policy deleted and recreated with a new UID can't satisfy the chain
	// until the declaration is updated; once it is, the new identity is used.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{
			{Name: "root"}, {Name: "parent"}, {Name: "first"}, {Name: "second"},
		}
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Delete(ctx, second))
	require.Eventually(t, func() bool {
		return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(second), second))
	}, waitTime, tickTime)
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	recreated := policy("second", `
    type: modifyHeaders
    parentPolicyRef: {name: parent}
    modifyHeaders:
      name: second
      config:
        actions: [{op: remove, remove: {key: x-second}}]`)
	require.NoError(t, cl.Create(ctx, recreated))
	require.NotEqual(t, second.UID, recreated.UID)
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	expectOrder(string(root.UID), string(parentPolicy.UID), string(first.UID), string(recreated.UID))

	// Restore a valid declaration, then delete only the chain: the policies
	// and their entities stay, in the default order.
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		g, ok, _ := pod.state(gatewayID(ct, gateway))
		if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
			return
		}
		got := make([]string, 0, len(g.entities.VirtualClusters[0].ProducePolicies))
		for _, p := range g.entities.VirtualClusters[0].ProducePolicies {
			got = append(got, p.Id.String())
		}
		assert.Len(ct, got, 4, "deleting the chain must not delete produce policy entities")
		assert.ElementsMatch(ct, []string{string(parentPolicy.UID), string(first.UID), string(recreated.UID), string(root.UID)}, got)
	}, waitTime, tickTime)
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{parentPolicy, first, recreated, root} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(p), p), "chain deletion does not delete policies")
	}
}

// TestOnPremEventGatewayProduceChainKeepsClusterChainOrderIndependent checks
// that the produce list and the cluster-level list of the same virtual cluster
// are ordered and compared independently.
func TestOnPremEventGatewayProduceChainKeepsClusterChainOrderIndependent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))
	pod := gwAdmin.provision(ctx)
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	backend := objectFromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, ns.Name, backendClusterYAML("backend", "backend", gateway.Name))
	parent := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	clusterPolicy := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, `
metadata: {name: cluster-policy}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: acls
    acls:
      name: cluster-policy
      config:
        rules:
        - action: allow
          resourceType: topic
          operations: [{name: read}]
          resourceNames: {type: stat, stat: [{match: orders}]}
`)
	producePolicy := func(name string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: `+name+`
      config:
        actions: [{op: remove, remove: {key: x-header}}]
`)
	}
	first, second := producePolicy("first"), producePolicy("second")
	clusterChain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: cluster-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: cluster-policy}]
`)
	produceChain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, ns.Name, `
metadata: {name: produce-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: second}, {name: first}]
`)
	for _, obj := range []client.Object{gateway, backend, parent, clusterPolicy, first, second, clusterChain, produceChain} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		g, ok, _ := pod.state(gatewayID(ct, gateway))
		if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
			return
		}
		vc := g.entities.VirtualClusters[0]
		if assert.Len(ct, vc.ProducePolicies, 2) {
			assert.Equal(ct, []string{string(second.UID), string(first.UID)},
				[]string{vc.ProducePolicies[0].Id.String(), vc.ProducePolicies[1].Id.String()})
		}
		if assert.Len(ct, vc.ClusterPolicies, 1) {
			assert.Equal(ct, string(clusterPolicy.UID), vc.ClusterPolicies[0].Id.String())
		}
	}, waitTime, tickTime)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		for _, obj := range []client.Object{clusterChain, produceChain} {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)) {
				return
			}
			cond := apimeta.FindStatusCondition(obj.(interface {
				GetConditions() []metav1.Condition
			}).GetConditions(), "Programmed")
			if assert.NotNil(ct, cond) {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}
	}, waitTime, tickTime)

	// Reordering the produce list must not make the cluster-level list order
	// significant, and the other way around: both chains stay Programmed.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(produceChain), produceChain); err != nil {
			return err
		}
		slices.Reverse(produceChain.Spec.APISpec.Policies)
		return cl.Update(ctx, produceChain)
	}))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		g, ok, _ := pod.state(gatewayID(ct, gateway))
		if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
			return
		}
		vc := g.entities.VirtualClusters[0]
		if assert.Len(ct, vc.ProducePolicies, 2) {
			assert.Equal(ct, []string{string(first.UID), string(second.UID)},
				[]string{vc.ProducePolicies[0].Id.String(), vc.ProducePolicies[1].Id.String()})
		}
	}, waitTime, tickTime)
	for _, obj := range []*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{clusterChain} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		cond := apimeta.FindStatusCondition(obj.GetConditions(), "Programmed")
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status, cond.Message)
	}
}
