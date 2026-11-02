package onprem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
)

func TestBuildClusterPolicyChainOwnership(t *testing.T) {
	gateway, chain, policies, objects := clusterPolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.Empty(t, cfg.invalidChains)
		return cfg
	}
	cfg := build()
	require.Equal(t, string(policies[1].UID), cfg.entities.VirtualClusters[0].ClusterPolicies[0].Id.String())
	require.Equal(t, "EventGatewayVirtualClusterPolicy "+ns+"/z-second", cfg.objects["virtual_clusters.0.cluster_policies.0"])
	require.Equal(t, "EventGatewayVirtualClusterPolicy "+ns+"/a-first", cfg.objects["virtual_clusters.0.cluster_policies.1"])
	require.Len(t, cfg.orderedClusterPolicies, 1)

	competitor := chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", types.UID(uuid.NewString()), ""
	competitor.CreationTimestamp = metav1.NewTime(chain.CreationTimestamp.Add(-time.Hour))
	slices.Reverse(competitor.Spec.APISpec.Policies)
	require.NoError(t, cl.Create(t.Context(), competitor))
	cfg = build()
	require.Equal(t, string(policies[1].UID), cfg.entities.VirtualClusters[0].ClusterPolicies[0].Id.String(), "an older competitor cannot steal a live claim")
	require.Len(t, cfg.excluded, 1)
	require.Contains(t, cfg.excluded[0].message, fmt.Sprintf("owned by policy chain %s", chain.Name))
	require.Equal(t, "ConflictingChain", cfg.excluded[0].conditions[0].Reason)

	// Terminating owners release their claim without waiting for finalizers.
	chain = read(t, cl, chain)
	chain.Finalizers = []string{"test.example/finalizer"}
	require.NoError(t, cl.Update(t.Context(), chain))
	require.NoError(t, cl.Delete(t.Context(), chain))
	cfg = build()
	require.Equal(t, string(policies[0].UID), cfg.entities.VirtualClusters[0].ClusterPolicies[0].Id.String())
	parent := &configurationv1alpha1.EventGatewayVirtualCluster{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, parent))
	require.Equal(t, policychain.ClaimValue(competitor), parent.Annotations[policychain.OwnerAnnotation])

	require.NoError(t, cl.Delete(t.Context(), competitor))
	recreated := competitor.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	build()
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policychain.ClaimValue(recreated), parent.Annotations[policychain.OwnerAnnotation], "a recreated chain must acquire its own UID-bound claim")
	require.NoError(t, cl.Delete(t.Context(), recreated))
	cfg = build()
	require.Empty(t, cfg.orderedClusterPolicies)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policychain.OwnerAnnotation)
	require.Len(t, cfg.entities.VirtualClusters[0].ClusterPolicies, 2, "deleting chains must never delete policy entities")
}

func TestReconcileClusterPolicyChainLifecycle(t *testing.T) {
	_, chain, policies, objects := clusterPolicyChainObjects(t)
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	for _, obj := range append(objects[1:], chain) {
		require.NoError(t, f.cl.Create(t.Context(), obj))
	}
	reconcile := func() {
		t.Helper()
		gateway := read(t, f.cl, f.gateway)
		_, err := f.r.Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		// This unit fake stores the request verbatim, unlike gw-admin.
		f.pod().mu.Lock()
		if held, ok := f.pod().gateways[f.gatewayID]; ok {
			f.pod().gateways[f.gatewayID] = withGWAdminDefaults(held)
		}
		f.pod().mu.Unlock()
	}
	heldIDs := func() []string {
		t.Helper()
		pod := f.pod()
		pod.mu.Lock()
		defer pod.mu.Unlock()
		held := pod.gateways[f.gatewayID].VirtualClusters
		require.Len(t, held, 1)
		ids := make([]string, 0, len(held[0].ClusterPolicies))
		for _, policy := range held[0].ClusterPolicies {
			ids = append(ids, policy.Id.String())
		}
		return ids
	}
	expectProgrammed := func(want metav1.ConditionStatus, reason string) {
		t.Helper()
		actual := read(t, f.cl, chain)
		cond := apimeta.FindStatusCondition(actual.Status.Conditions, "Programmed")
		require.NotNil(t, cond)
		require.Equal(t, want, cond.Status, cond.Message)
		require.Equal(t, reason, cond.Reason, cond.Message)
		require.Empty(t, actual.GetKonnectID())
	}
	reconcile()
	require.Equal(t, []string{string(policies[1].UID), string(policies[0].UID)}, heldIDs())
	expectProgrammed(metav1.ConditionTrue, "Programmed")
	first := read(t, f.cl, f.gateway)

	chain = read(t, f.cl, chain)
	slices.Reverse(chain.Spec.APISpec.Policies)
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	f.pod().reset()
	reconcile()
	require.Contains(t, f.pod().recorded(), callEntities, "changing only order must push a new document")
	require.Equal(t, []string{string(policies[0].UID), string(policies[1].UID)}, heldIDs())
	second := read(t, f.cl, f.gateway)
	require.Greater(t, second.Status.ConfigRevision, first.Status.ConfigRevision)
	require.NotEqual(t, second.Status.ConfigHash, first.Status.ConfigHash)

	f.pod().mu.Lock()
	remote := f.pod().gateways[f.gatewayID]
	slices.Reverse(remote.VirtualClusters[0].ClusterPolicies)
	f.pod().gateways[f.gatewayID] = remote
	f.pod().mu.Unlock()
	f.pod().reset()
	reconcile()
	require.Contains(t, f.pod().recorded(), callEntities, "a changed remote order is repaired even at the same revision")
	require.Equal(t, second.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Equal(t, []string{string(policies[0].UID), string(policies[1].UID)}, heldIDs())

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:1]
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	f.pod().reset()
	reconcile()
	require.Empty(t, f.pod().recorded(), "an incomplete owner freezes the whole document, including its revision")
	require.Equal(t, second.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Equal(t, second.Status.ConfigHash, read(t, f.cl, f.gateway).Status.ConfigHash)
	expectProgrammed(metav1.ConditionFalse, "Invalid")
	require.Equal(t, "InvalidPolicyChain", apimeta.FindStatusCondition(read(t, f.cl, f.gateway).Status.Conditions, "Programmed").Reason)

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{{Name: policies[1].Name}, {Name: policies[0].Name}}
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	reconcile()
	expectProgrammed(metav1.ConditionTrue, "Programmed")

	oldUID := policies[1].UID
	require.NoError(t, f.cl.Delete(t.Context(), policies[1]))
	reconcile()
	expectProgrammed(metav1.ConditionFalse, "Invalid")
	recreated := policies[1].DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, f.cl.Create(t.Context(), recreated))
	reconcile()
	expectProgrammed(metav1.ConditionTrue, "Programmed")
	require.Equal(t, []string{string(recreated.UID), string(policies[0].UID)}, heldIDs())
	require.NotContains(t, heldIDs(), string(oldUID), "same-name replacement must use the new native configuration ID")

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, chain)))
	reconcile()
	require.Equal(t, []string{string(policies[0].UID), string(recreated.UID)}, heldIDs(), "deletion restores the creation/name default order")
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterPolicy{policies[0], recreated} {
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(p), p), "chain deletion does not delete policy CRs")
	}
	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "no-chain configurations must not be pushed repeatedly")
}

func clusterPolicyChainObjects(t *testing.T) (
	*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain,
	[]*configurationv1alpha1.EventGatewayVirtualClusterPolicy, []client.Object,
) {
	t.Helper()
	created := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
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
	parent := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	policies := make([]*configurationv1alpha1.EventGatewayVirtualClusterPolicy, 0, 2)
	objects := []client.Object{gateway, backend, parent}
	for i, name := range []string{"a-first", "z-second"} {
		policy := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, created.Add(time.Duration(i)*time.Minute), `
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
          resourceNames: {type: stat, stat: [{match: orders}]}
`)
		policies = append(policies, policy)
		objects = append(objects, policy)
	}
	chain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, created, `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies:
    - {name: z-second, kind: EventGatewayVirtualClusterPolicy, namespace: default}
    - {name: a-first}
`)
	return gateway, chain, policies, objects
}

func TestClusterPolicyOrderValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		change  func(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, []*configurationv1alpha1.EventGatewayVirtualClusterPolicy)
		problem string
	}{
		{name: "declared order without Konnect IDs"},
		{name: "omitted list", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies = nil
		}, problem: "must be supplied explicitly"},
		{name: "empty nonempty scope", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
		}, problem: "must list every cluster-level policy"},
		{name: "partial declaration", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies = c.Spec.APISpec.Policies[:1]
		}, problem: "a-first is missing"},
		{name: "duplicate", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[0]
		}, problem: "duplicates policy"},
		{name: "missing", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Name = "missing"
		}, problem: "does not exist"},
		{name: "wrong kind", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, problem: "must name an EventGatewayVirtualClusterPolicy"},
		{name: "cross namespace policy", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, problem: "same namespace as the chain"},
		{name: "cross namespace parent", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("other")
		}, problem: "same namespace"},
		{name: "wrong parent", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, p []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			p[1].Spec.EventGatewayVirtualClusterRef = namespacedRef("other-vc")
		}, problem: "different virtual cluster"},
		{name: "terminating policy", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, p []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			p[1].DeletionTimestamp = new(metav1.Now())
			p[1].Finalizers = []string{"test.example/finalizer"}
		}, problem: "being deleted"},
		{name: "excluded policy", change: func(_ *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, p []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			p[1].UID = types.UID("not-a-uuid")
		}, problem: "not in the valid configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := clusterPolicyChainObjects(t)
			if tt.change != nil {
				tt.change(chain, policies)
			}
			cl := newFakeClient(t, objects...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			ordered, problem, err := cfg.clusterPolicyOrder(t.Context(), cl, chain)
			require.NoError(t, err)
			if tt.problem != "" {
				require.Contains(t, problem, tt.problem)
				require.Nil(t, ordered, "an invalid reference cannot result in a shortened chain")
				return
			}
			require.Empty(t, problem)
			require.Len(t, ordered, 2)
			require.Equal(t, uuid.MustParse(string(policies[1].UID)), ordered[0].Id)
			require.Equal(t, uuid.MustParse(string(policies[0].UID)), ordered[1].Id)
			require.Empty(t, chain.GetKonnectID())
		})
	}
}

func TestClusterPolicyChainUsesAssembledParentPolicies(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "policy removed during assembly", true: "policy moved during assembly"}[moved], func(t *testing.T) {
			gateway, chain, policies, objects := clusterPolicyChainObjects(t)
			chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{{Name: policies[0].Name}}
			if moved {
				parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster).DeepCopy()
				parent.Name, parent.UID = "other-vc", types.UID(uuid.NewString())
				parent.Spec.APISpec.Name = "other-vc"
				objects = append(objects, parent)
				policies[1].Spec.EventGatewayVirtualClusterRef = namespacedRef(parent.Name)
				chain.Spec.APISpec.Policies[0].Name = policies[1].Name
			}
			resolving := false
			cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if p, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicy); ok {
						resolving = true
						if moved {
							p.Spec.EventGatewayVirtualClusterRef = namespacedRef("vc")
						}
					}
					return nil
				},
				List: func(ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
					if err := c.List(ctx, obj, opts...); err != nil {
						return err
					}
					if list, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicyList); ok && resolving {
						policy := policies[0].DeepCopy()
						if moved {
							policy = policies[1].DeepCopy()
							policy.Spec.EventGatewayVirtualClusterRef = namespacedRef("vc")
						}
						list.Items = []configurationv1alpha1.EventGatewayVirtualClusterPolicy{*policy}
					}
					return nil
				},
			}, append(objects, chain)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.Len(t, cfg.invalidChains, 1)
			require.Contains(t, cfg.invalidChains[0], "policy set changed during configuration assembly")
			require.Empty(t, cfg.orderedClusterPolicies)
		})
	}
}

func TestClusterPolicyOrderEmptyAndReadErrors(t *testing.T) {
	gateway, chain, policies, objects := clusterPolicyChainObjects(t)
	cl := newFakeClient(t, objects...)
	for _, p := range policies {
		require.NoError(t, cl.Delete(t.Context(), p))
	}
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	ordered, problem, err := cfg.clusterPolicyOrder(t.Context(), cl, chain)
	require.NoError(t, err)
	require.Empty(t, problem)
	require.NotNil(t, ordered)
	require.Empty(t, ordered)

	failure := errors.New("API unavailable")
	for _, failList := range []bool{false, true} {
		t.Run(map[bool]string{false: "get", true: "list"}[failList], func(t *testing.T) {
			_, chain, _, objects := clusterPolicyChainObjects(t)
			if failList {
				chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
			}
			broken := newFakeClientWithInterceptor(t, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if !failList {
						return failure
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return failure },
			}, objects...)
			ordered, problem, err := cfg.clusterPolicyOrder(t.Context(), broken, chain)
			require.ErrorIs(t, err, failure)
			require.Empty(t, problem, "API failures must be retried, not mistaken for invalid or empty configuration")
			require.Nil(t, ordered)
		})
	}
}
