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
	require.Len(t, cfg.orderedPolicies, 1)

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
	require.Empty(t, cfg.orderedPolicies)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policychain.OwnerAnnotation)
	require.Len(t, cfg.entities.VirtualClusters[0].ClusterPolicies, 2, "deleting chains must never delete policy entities")
}

func TestBuildClusterPolicyChainExcludedParentOwnership(t *testing.T) {
	for _, tt := range []struct {
		name        string
		claim       func(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string
		terminating bool
		blocked     bool
	}{
		{name: "unclaimed"},
		{name: "live owner and competitor", claim: policychain.ClaimValue[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain], blocked: true},
		{name: "recreated owner", claim: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
			return c.Name + "/" + uuid.NewString()
		}},
		{name: "missing owner", claim: func(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
			return "missing/" + uuid.NewString()
		}},
		{name: "malformed claim", claim: func(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
			return "malformed"
		}},
		{name: "terminating owner", claim: policychain.ClaimValue[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain], terminating: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, chain, _, objects := clusterPolicyChainObjects(t)
			parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
			if tt.claim != nil {
				parent.Annotations = map[string]string{policychain.OwnerAnnotation: tt.claim(chain)}
			}
			competitor := chain.DeepCopy()
			competitor.Name, competitor.UID = "competitor", types.UID(uuid.NewString())
			if tt.terminating {
				chain.DeletionTimestamp = new(metav1.Now())
				chain.Finalizers = []string{"test.example/finalizer"}
			}
			validParent := parent.DeepCopy()
			validParent.Name, validParent.UID = "valid-vc", types.UID(uuid.NewString())
			validParent.Annotations = nil
			validParent.CreationTimestamp = metav1.NewTime(parent.CreationTimestamp.Add(-time.Hour))
			f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
				return append(append(f.gwAdmin.Objects(), f.gateway), append(objects[1:], validParent, chain, competitor)...)
			})
			cl := f.cl
			cfg, err := buildConfiguration(t.Context(), cl, f.gateway, "", nil)
			require.NoError(t, err)
			require.Len(t, cfg.entities.VirtualClusters, 1)
			require.Equal(t, string(validParent.UID), cfg.entities.VirtualClusters[0].Id.String())
			require.Empty(t, cfg.orderedPolicies)
			if tt.blocked {
				require.Equal(t, []string{client.ObjectKeyFromObject(chain).String() + ": its EventGatewayVirtualCluster " +
					client.ObjectKeyFromObject(parent).String() + " isn't in the configuration"}, cfg.invalidChains,
					"only the UID-bound owner may block, not its competitor")
				require.Equal(t, policychain.ClaimValue(chain), read(t, cl, parent).Annotations[policychain.OwnerAnnotation])
			} else {
				require.Empty(t, cfg.invalidChains)
				require.NotContains(t, read(t, cl, parent).Annotations, policychain.OwnerAnnotation,
					"an excluded parent must not acquire a new claim after releasing an inactive owner")
			}
			_, err = f.r.Reconcile(t.Context(), read(t, cl, f.gateway))
			require.NoError(t, err)
			if tt.blocked {
				require.Empty(t, f.pod().recorded())
			} else {
				require.Contains(t, f.pod().recorded(), callEntities, "an unowned excluded scope must not block the gateway")
			}
			condition := apimeta.FindStatusCondition(read(t, cl, competitor).Status.Conditions, "Programmed")
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Contains(t, condition.Message, "isn't in the configuration")
		})
	}
}

func TestReconcileClusterPolicyChainParentLifecycle(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "excluded owning parent", true: "deleted owning parent"}[deleted], func(t *testing.T) {
			_, chain, policies, objects := clusterPolicyChainObjects(t)
			parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
			f := newPushFixture(t, interceptor.Funcs{}, nil)
			for _, obj := range append(objects[1:], chain) {
				require.NoError(t, f.cl.Create(t.Context(), obj))
			}
			duplicate := parent.DeepCopy()
			duplicate.Name, duplicate.UID, duplicate.ResourceVersion = "other-vc", types.UID(uuid.NewString()), ""
			duplicate.Spec.APISpec.Name = "other-vc"
			duplicate.Spec.APISpec.DNSLabel = "other-vc"
			duplicate.CreationTimestamp = metav1.NewTime(parent.CreationTimestamp.Add(-time.Hour))
			if !deleted {
				require.NoError(t, f.cl.Create(t.Context(), duplicate))
			}
			reconcile := func() {
				t.Helper()
				_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
				require.NoError(t, err)
			}
			reconcile()
			first := read(t, f.cl, f.gateway)
			require.Equal(t, policychain.ClaimValue(chain), read(t, f.cl, parent).Annotations[policychain.OwnerAnnotation])
			if deleted {
				require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, parent)))
			} else {
				backend := read(t, f.cl, objects[1].(*configurationv1alpha1.EventGatewayBackendCluster))
				backend.Spec.APISpec.Name = "changed-backend"
				require.NoError(t, f.cl.Update(t.Context(), backend))
				duplicate = read(t, f.cl, duplicate)
				duplicate.Spec.APISpec.Name = parent.Spec.APISpec.Name
				require.NoError(t, f.cl.Update(t.Context(), duplicate))
			}
			f.pod().reset()
			reconcile()
			current := read(t, f.cl, f.gateway)
			condition := apimeta.FindStatusCondition(current.Status.Conditions, "Programmed")
			require.NotNil(t, condition)
			if deleted {
				require.Contains(t, f.pod().recorded(), callEntities)
				require.Greater(t, current.Status.ConfigRevision, first.Status.ConfigRevision)
				require.Equal(t, metav1.ConditionTrue, condition.Status, condition.Message)
				f.pod().mu.Lock()
				held := f.pod().gateways[f.gatewayID]
				f.pod().mu.Unlock()
				require.Empty(t, held.VirtualClusters)
				require.Equal(t, "Detached", apimeta.FindStatusCondition(read(t, f.cl, chain).Status.Conditions, "Programmed").Reason)
			} else {
				require.Empty(t, f.pod().recorded(), "an excluded owning scope must retain the last running document")
				require.Equal(t, first.Status.ConfigRevision, current.Status.ConfigRevision)
				require.Equal(t, first.Status.ConfigHash, current.Status.ConfigHash)
				require.Equal(t, "InvalidPolicyChain", condition.Reason)
				require.Equal(t, "Invalid", apimeta.FindStatusCondition(read(t, f.cl, chain).Status.Conditions, "Programmed").Reason)
				require.NoError(t, f.cl.Delete(t.Context(), duplicate))
				reconcile()
				require.Contains(t, f.pod().recorded(), callEntities, "repairing the owning scope must resume pending updates")
				require.Greater(t, read(t, f.cl, f.gateway).Status.ConfigRevision, first.Status.ConfigRevision)
			}
			for _, policy := range policies {
				require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(policy), policy))
			}
		})
	}
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

func producePolicyChainObjects(t *testing.T) (
	*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
	[]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, []client.Object,
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
	policies := make([]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, 0, 2)
	objects := []client.Object{gateway, backend, parent}
	for i, name := range []string{"a-first", "z-second"} {
		policy := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created.Add(time.Duration(i)*time.Minute), `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: `+name+`
      config:
        actions:
        - op: set
          set: {key: x-stage, value: `+name+`}
`)
		policies = append(policies, policy)
		objects = append(objects, policy)
	}
	chain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t, created, `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies:
    - {name: z-second}
    - {name: a-first}
`)
	return gateway, chain, policies, objects
}

func TestBuildProducePolicyChainOrder(t *testing.T) {
	gateway, chain, policies, objects := producePolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.invalidChains)
	require.Equal(t, string(policies[1].UID), cfg.entities.VirtualClusters[0].ProducePolicies[0].Id.String())
	require.Equal(t, "EventGatewayVirtualClusterProducePolicy "+ns+"/a-first", cfg.objects["virtual_clusters.0.produce_policies.1"])
	require.Equal(t, []uuid.UUID{uuid.MustParse(string(objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster).UID))},
		cfg.orderedProducePolicies, "only the produce list of the virtual cluster is order-sensitive")
	require.Empty(t, cfg.orderedPolicies, "a produce chain doesn't order the cluster-level list")
}

func TestBuildProducePolicyChainOwnershipIsIndependent(t *testing.T) {
	gateway, chain, _, objects := producePolicyChainObjects(t)
	parent := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
	cl := newFakeClient(t, append(objects, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.Empty(t, cfg.invalidChains)
		return cfg
	}
	build()
	claimed := read(t, cl, parent)
	require.Equal(t, policychain.ClaimValue(chain), claimed.Annotations[policychain.ProduceOwnerAnnotation])
	require.Empty(t, claimed.Annotations[policychain.OwnerAnnotation], "produce ownership is independent of the cluster chain")

	// A cluster chain of the same virtual cluster can claim it too.
	clusterChain := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, time.Now().UTC(), `
metadata: {name: cluster-chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: []
`)
	require.NoError(t, cl.Create(t.Context(), clusterChain))
	cfg := build()
	require.Empty(t, cfg.invalidChains, "a cluster chain over an empty cluster-level set is valid")
	claimed = read(t, cl, parent)
	require.Equal(t, policychain.ClaimValue(clusterChain), claimed.Annotations[policychain.OwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(chain), claimed.Annotations[policychain.ProduceOwnerAnnotation])
}

func TestBuildProducePolicyChainOrderValidation(t *testing.T) {
	for _, tt := range []struct {
		name, problem string
		change        func(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain,
			[]*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy)
	}{
		{name: "declared order"},
		{name: "omitted list", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = nil
		}, problem: "must be supplied explicitly"},
		{name: "empty nonempty scope", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}
		}, problem: "must list every produce policy"},
		{name: "partial declaration", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = c.Spec.APISpec.Policies[:1]
		}, problem: "a-first is missing"},
		{name: "duplicate", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[0]
		}, problem: "duplicates policy"},
		{name: "missing", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0].Name = "missing"
		}, problem: "does not exist"},
		{name: "wrong kind", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, problem: "must name an EventGatewayVirtualClusterProducePolicy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := producePolicyChainObjects(t)
			if tt.change != nil {
				tt.change(chain, policies)
			}
			cl := newFakeClient(t, append(objects, chain)...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			if tt.problem == "" {
				require.Empty(t, cfg.invalidChains)
				return
			}
			require.NotEmpty(t, cfg.invalidChains)
			require.Contains(t, cfg.invalidChains[0], tt.problem)
		})
	}
}

// TestProducePolicyOrderKeepsReferencesLocal checks the produce chain contract
// the API declares: unlike the cluster-level chain, its parent and policies
// are never of another namespace, whatever a KongReferenceGrant allows.
func TestProducePolicyOrderKeepsReferencesLocal(t *testing.T) {
	policy := func(name string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
			Name: name, Namespace: ns,
			Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
					EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
						Type:                      configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate,
						ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{},
					},
				},
			},
		}
	}
	chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
		Name: "chain", Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{
			EventGatewayVirtualClusterRef: namespacedRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{
					{Name: "first", Namespace: "other"},
				},
			},
		},
	}
	problem := producePolicyOrder(chain, []policyRef{{name: "first", namespace: "other"}}, []entityWithParent{policy("first")})
	require.Contains(t, problem, "not cross-namespace")

	chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("other")
	chain.Spec.APISpec.Policies[0].Namespace = ""
	problem = producePolicyOrder(chain, []policyRef{{name: "first"}}, []entityWithParent{policy("first")})
	require.Contains(t, problem, "in the same namespace")
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
		{name: "cross namespace policy without a KongReferenceGrant", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, problem: "no KongReferenceGrant allows it to reference the EventGatewayVirtualClusterPolicy other/z-second"},
		{name: "cross namespace parent of other policies", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, _ []*configurationv1alpha1.EventGatewayVirtualClusterPolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("other")
		}, problem: "different virtual cluster"},
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
			ordered, problem, err := policyOrder(t.Context(), &cfg, cl, chain, clusterPolicyChains)
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

// TestClusterPolicyOrderAcrossNamespaces checks a chain orders the policies of
// its virtual cluster of other namespaces a KongReferenceGrant lets it
// reference, and needn't list those no KongReferenceGrant lets reference the
// virtual cluster: they are left out of the configuration.
func TestClusterPolicyOrderAcrossNamespaces(t *testing.T) {
	const other = "other"
	// grant lets objects of fromKind in fromNamespace reference objects of
	// toKind in namespace.
	grant := func(namespace, fromNamespace, fromKind, toKind string) *configurationv1alpha1.KongReferenceGrant {
		return &configurationv1alpha1.KongReferenceGrant{
			Name: fromKind + "-to-" + toKind, Namespace: namespace,
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{
					Group: "configuration.konghq.com", Kind: configurationv1alpha1.Kind(fromKind), Namespace: configurationv1alpha1.Namespace(fromNamespace),
				}},
				To: []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: configurationv1alpha1.Kind(toKind)}},
			},
		}
	}
	// A policy of the other namespace, attached to the virtual cluster of
	// the gateway's namespace.
	otherPolicy := func(t *testing.T) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
		return fromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC), `
metadata: {name: m-other, namespace: `+other+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc, namespace: `+ns+`}}
  apiSpec:
    type: acls
    acls:
      name: m-other
      config:
        rules:
        - action: allow
          resourceType: topic
          operations: [{name: read}]
          resourceNames: {type: stat, stat: [{match: orders}]}
`)
	}

	t.Run("a granted policy of another namespace is ordered", func(t *testing.T) {
		gateway, chain, policies, objects := clusterPolicyChainObjects(t)
		policy := otherPolicy(t)
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: policy.Name, Namespace: other})
		cl := newFakeClient(t, append(objects, policy,
			grant(ns, other, "EventGatewayVirtualClusterPolicy", "EventGatewayVirtualCluster"),
			grant(other, ns, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualClusterPolicy"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		ordered, problem, err := policyOrder(t.Context(), &cfg, cl, chain, clusterPolicyChains)
		require.NoError(t, err)
		require.Empty(t, problem)
		require.Len(t, ordered, 3)
		require.Equal(t, uuid.MustParse(string(policies[1].UID)), ordered[0].Id)
		require.Equal(t, uuid.MustParse(string(policies[0].UID)), ordered[1].Id)
		require.Equal(t, uuid.MustParse(string(policy.UID)), ordered[2].Id)
	})

	t.Run("a granted policy of another namespace must be listed", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		cl := newFakeClient(t, append(objects, otherPolicy(t),
			grant(ns, other, "EventGatewayVirtualClusterPolicy", "EventGatewayVirtualCluster"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		_, problem, err := policyOrder(t.Context(), &cfg, cl, chain, clusterPolicyChains)
		require.NoError(t, err)
		require.Contains(t, problem, "policy "+other+"/m-other is missing")
	})

	t.Run("a policy of another namespace not let reference the virtual cluster needn't be listed", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		cl := newFakeClient(t, append(objects, otherPolicy(t))...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		ordered, problem, err := policyOrder(t.Context(), &cfg, cl, chain, clusterPolicyChains)
		require.NoError(t, err)
		require.Empty(t, problem)
		require.Len(t, ordered, 2)
	})

	t.Run("a granted chain of another namespace claims the virtual cluster", func(t *testing.T) {
		gateway, chain, policies, objects := clusterPolicyChainObjects(t)
		chain.Namespace = other
		chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
		for i := range chain.Spec.APISpec.Policies {
			chain.Spec.APISpec.Policies[i].Namespace = ns
		}
		cl := newFakeClient(t, append(objects, chain,
			grant(ns, other, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualCluster"),
			grant(ns, other, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualClusterPolicy"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.invalidChains)
		require.Len(t, cfg.orderedPolicies, 1)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.Equal(t, other+"/chain/"+string(chain.UID), vc.Annotations[policychain.OwnerAnnotation])
		require.Equal(t, uuid.MustParse(string(policies[1].UID)), cfg.entities.VirtualClusters[0].ClusterPolicies[0].Id)
	})

	t.Run("a chain without a KongReferenceGrant to its policies reports it and blocks", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		chain.Namespace = other
		chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
		for i := range chain.Spec.APISpec.Policies {
			chain.Spec.APISpec.Policies[i].Namespace = ns
		}
		cl := newFakeClient(t, append(objects, chain,
			grant(ns, other, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualCluster"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NotEmpty(t, cfg.invalidChains)
		require.Len(t, cfg.excluded, 1)
		require.Equal(t, eventgatewayv1alpha1.OnPremEntityReferenceNotPermittedReason, cfg.excluded[0].reason)
		require.Contains(t, cfg.excluded[0].message, "no KongReferenceGrant allows it to reference the EventGatewayVirtualClusterPolicy")
		require.Empty(t, cfg.excluded[0].conditions, "it doesn't claim the virtual cluster, as against Konnect")
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.NotContains(t, vc.Annotations, policychain.OwnerAnnotation)
	})

	// otherChain returns an older chain of the other namespace for the
	// virtual cluster, ordering its policies.
	otherChain := func(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
		c := chain.DeepCopy()
		c.Namespace = other
		c.UID = types.UID(uuid.NewString())
		c.CreationTimestamp = metav1.NewTime(chain.CreationTimestamp.Add(-time.Hour))
		c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
		for i := range c.Spec.APISpec.Policies {
			c.Spec.APISpec.Policies[i].Namespace = ns
		}
		return c
	}

	t.Run("an older chain without a KongReferenceGrant to its policies leaves the virtual cluster to a valid one", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		older := otherChain(chain)
		cl := newFakeClient(t, append(objects, chain, older,
			grant(ns, other, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualCluster"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.invalidChains)
		require.Len(t, cfg.orderedPolicies, 1)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.Equal(t, "chain/"+string(chain.UID), vc.Annotations[policychain.OwnerAnnotation])
		require.Equal(t, map[string]string{
			"EventGatewayVirtualClusterPolicyChain chain": "no KongReferenceGrant allows it to reference the EventGatewayVirtualClusterPolicy " + ns + "/z-second",
		}, excludedMessages(cfg))
	})

	t.Run("the claim of a chain of another namespace whose KongReferenceGrant is revoked is released", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		older := otherChain(chain)
		vc := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
		vc.Annotations = map[string]string{policychain.OwnerAnnotation: policychain.ClaimValue(older)}
		cl := newFakeClient(t, append(objects, chain, older)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.invalidChains)
		require.Len(t, cfg.orderedPolicies, 1, "the chain of the namespace of the virtual cluster orders it")
		var got configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(vc), &got))
		require.Equal(t, "chain/"+string(chain.UID), got.Annotations[policychain.OwnerAnnotation])
	})

	t.Run("a chain losing the claim to an owner without a KongReferenceGrant to its policies doesn't release the push", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		// The owner keeps its claim, as it may still reference the virtual
		// cluster, but can't order its policies.
		owner := otherChain(chain)
		vc := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster)
		vc.Annotations = map[string]string{policychain.OwnerAnnotation: policychain.ClaimValue(owner)}
		cl := newFakeClient(t, append(objects, chain, owner,
			grant(ns, other, "EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualCluster"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.Empty(t, cfg.orderedPolicies)
		require.Equal(t, []string{
			other + "/chain: no KongReferenceGrant allows it to reference the EventGatewayVirtualClusterPolicy " + ns + "/z-second",
		}, cfg.invalidChains, "the owner's policies would keep their default order")
		var got configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(vc), &got))
		require.Equal(t, policychain.ClaimValue(owner), got.Annotations[policychain.OwnerAnnotation])
	})

	t.Run("a virtual cluster of another namespace not let reference its backend cluster doesn't hold back the push", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		foreign := objects[2].(*configurationv1alpha1.EventGatewayVirtualCluster).DeepCopy()
		foreign.Namespace, foreign.UID = other, types.UID(uuid.NewString())
		foreign.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new(ns)
		foreignChain := chain.DeepCopy()
		foreignChain.Namespace, foreignChain.UID = other, types.UID(uuid.NewString())
		// It owns its virtual cluster, and can't order it: the cluster isn't
		// in the configuration.
		foreign.Annotations = map[string]string{policychain.OwnerAnnotation: policychain.ClaimValue(foreignChain)}
		cl := newFakeClient(t, append(objects, chain, foreign, foreignChain)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.invalidChains)
		var got configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), &got))
		require.Equal(t, policychain.ClaimValue(foreignChain), got.Annotations[policychain.OwnerAnnotation], "the claim isn't the gateway's to release")
	})

	t.Run("a chain of another namespace without a KongReferenceGrant is left out without claiming", func(t *testing.T) {
		gateway, chain, _, objects := clusterPolicyChainObjects(t)
		chain.Namespace = other
		chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
		cl := newFakeClient(t, append(objects, chain)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.orderedPolicies)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.NotContains(t, vc.Annotations, policychain.OwnerAnnotation)
		require.Equal(t, map[string]string{
			"EventGatewayVirtualClusterPolicyChain chain": "no KongReferenceGrant allows it to reference the EventGatewayVirtualCluster " + ns + "/vc",
		}, excludedMessages(cfg))
	})
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
				parent.Spec.APISpec.DNSLabel = "other-vc"
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
			require.Empty(t, cfg.orderedPolicies)
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
	ordered, problem, err := policyOrder(t.Context(), &cfg, cl, chain, clusterPolicyChains)
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
			ordered, problem, err := policyOrder(t.Context(), &cfg, broken, chain, clusterPolicyChains)
			require.ErrorIs(t, err, failure)
			require.Empty(t, problem, "API failures must be retried, not mistaken for invalid or empty configuration")
			require.Nil(t, ordered)
		})
	}
}
