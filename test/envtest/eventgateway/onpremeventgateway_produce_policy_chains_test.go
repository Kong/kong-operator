package eventgateway

import (
	"cmp"
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
	defaultOrder := func(policies ...*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) []string {
		t.Helper()
		remaining := slices.Clone(policies)
		slices.SortFunc(remaining, func(a, b *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) int {
			return cmp.Or(
				a.CreationTimestamp.Compare(b.CreationTimestamp.Time),
				cmp.Compare(a.Name, b.Name),
				cmp.Compare(a.Namespace, b.Namespace),
			)
		})
		added := map[string]bool{}
		ids := make([]string, 0, len(remaining))
		// A root opens its group: its children follow it before the next root,
		// as gw-admin requires a parent's children to stay contiguous.
		var addChildren func(parent string)
		addChildren = func(parent string) {
			for {
				i := slices.IndexFunc(remaining, func(p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) bool {
					return p.Spec.APISpec.ParentPolicyRef.Name == parent
				})
				if i < 0 {
					return
				}
				p := remaining[i]
				remaining = slices.Delete(remaining, i, i+1)
				ids = append(ids, string(p.UID))
				added[p.Name] = true
				addChildren(p.Name)
			}
		}
		for len(remaining) > 0 {
			i := slices.IndexFunc(remaining, func(p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) bool {
				parent := p.Spec.APISpec.ParentPolicyRef.Name
				return parent == "" || added[parent]
			})
			require.GreaterOrEqual(t, i, 0, "policies must form an acyclic parent group")
			p := remaining[i]
			remaining = slices.Delete(remaining, i, i+1)
			ids = append(ids, string(p.UID))
			added[p.Name] = true
			addChildren(p.Name)
		}
		return ids
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
	// The parent opens a schemaValidation group; first and second are its
	// modifyHeaders children, and root is an independent modifyHeaders policy.
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

	// Order the children differently from the default order, then delete only
	// the chain: the default order must be restored, and the policies and their
	// entities must stay.
	defaultIDs := defaultOrder(root, parentPolicy, first, recreated)
	names := map[string]string{
		string(root.UID):         "root",
		string(parentPolicy.UID): "parent",
		string(first.UID):        "first",
		string(recreated.UID):    "second",
	}
	candidates := [][]string{
		{string(root.UID), string(parentPolicy.UID), string(recreated.UID), string(first.UID)},
		{string(root.UID), string(parentPolicy.UID), string(first.UID), string(recreated.UID)},
		{string(parentPolicy.UID), string(recreated.UID), string(first.UID), string(root.UID)},
		{string(parentPolicy.UID), string(first.UID), string(recreated.UID), string(root.UID)},
	}
	var chainIDs []string
	for _, candidate := range candidates {
		if !slices.Equal(candidate, defaultIDs) {
			chainIDs = candidate
			break
		}
	}
	require.NotNil(t, chainIDs, "need a declaration order different from the default order")
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		chain.Spec.APISpec.Policies = nil
		for _, id := range chainIDs {
			chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies,
				configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: names[id]})
		}
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	expectOrder(chainIDs...)
	require.NoError(t, cl.Delete(ctx, chain))
	expectOrder(defaultIDs...)
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
	clusterPolicy := func(name string) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
		return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: acls
    acls:
      name: `+name+`
      config:
        rules:
        - action: allow
          resourceType: topic
          operations: [{name: read}]
          resourceNames: {type: stat, stat: [{match: `+name+`}]}
`)
	}
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
	clusterFirst, clusterSecond := clusterPolicy("cluster-first"), clusterPolicy("cluster-second")
	first, second := producePolicy("first"), producePolicy("second")
	clusterChain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: cluster-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: cluster-second}, {name: cluster-first}]
`)
	produceChain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, ns.Name, `
metadata: {name: produce-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: second}, {name: first}]
`)
	for _, obj := range []client.Object{gateway, backend, parent, clusterFirst, clusterSecond, first, second, clusterChain, produceChain} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	expectProgrammed := func() {
		t.Helper()
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
	}
	expectOrders := func(produce, cluster []string, after int) int {
		t.Helper()
		version := 0
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g, ok, _ := pod.state(gatewayID(ct, gateway))
			if !assert.True(ct, ok) || !assert.Len(ct, g.entities.VirtualClusters, 1) {
				return
			}
			vc := g.entities.VirtualClusters[0]
			gotProduce := make([]string, 0, len(vc.ProducePolicies))
			for _, p := range vc.ProducePolicies {
				gotProduce = append(gotProduce, p.Id.String())
			}
			gotCluster := make([]string, 0, len(vc.ClusterPolicies))
			for _, p := range vc.ClusterPolicies {
				gotCluster = append(gotCluster, p.Id.String())
			}
			assert.Equal(ct, produce, gotProduce)
			assert.Equal(ct, cluster, gotCluster)
			version = g.version.DataVersion
			if after > 0 {
				assert.Greater(ct, version, after, "the reorder must be pushed")
			}
		}, waitTime, tickTime)
		return version
	}
	clusterOrder := []string{string(clusterSecond.UID), string(clusterFirst.UID)}
	expectProgrammed()
	version := expectOrders([]string{string(second.UID), string(first.UID)}, clusterOrder, 0)

	// Reordering the produce list must not make the cluster-level list order
	// significant.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(produceChain), produceChain); err != nil {
			return err
		}
		slices.Reverse(produceChain.Spec.APISpec.Policies)
		return cl.Update(ctx, produceChain)
	}))
	expectProgrammed()
	version = expectOrders([]string{string(first.UID), string(second.UID)}, clusterOrder, version)

	// Reordering the cluster-level list must not make the produce list order
	// significant.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(clusterChain), clusterChain); err != nil {
			return err
		}
		slices.Reverse(clusterChain.Spec.APISpec.Policies)
		return cl.Update(ctx, clusterChain)
	}))
	expectProgrammed()
	expectOrders([]string{string(first.UID), string(second.UID)}, []string{string(clusterFirst.UID), string(clusterSecond.UID)}, version)
}
