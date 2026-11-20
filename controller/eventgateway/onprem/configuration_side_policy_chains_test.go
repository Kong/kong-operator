package onprem

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
)

// chainGrant lets objects of fromKind in the namespace "other" reference
// objects of toKind in the namespace of the gateway.
func chainGrant(fromKind, toKind string) *configurationv1alpha1.KongReferenceGrant {
	return grantIn(ns, "other", fromKind, toKind)
}

// grantIn lets objects of fromKind in fromNamespace reference objects of
// toKind in namespace.
func grantIn(namespace, fromNamespace, fromKind, toKind string) *configurationv1alpha1.KongReferenceGrant {
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

// TestBuildProducePolicyChainOfAnotherNamespace checks a produce chain of
// another namespace than its virtual cluster and policies orders them with
// KongReferenceGrants, and without one for its policies reports it and
// doesn't claim the virtual cluster.
func TestBuildProducePolicyChainOfAnotherNamespace(t *testing.T) {
	const (
		other      = "other"
		chainKind  = "EventGatewayVirtualClusterProducePolicyChain"
		policyKind = "EventGatewayVirtualClusterProducePolicy"
	)
	moved := func(t *testing.T) (*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, []client.Object) {
		gateway, chain, policies, objects := producePolicyChainObjects(t)
		chain.Namespace = other
		chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
		for i := range chain.Spec.APISpec.Policies {
			chain.Spec.APISpec.Policies[i].Namespace = ns
		}
		return gateway, chain, policies, objects
	}

	t.Run("granted", func(t *testing.T) {
		gateway, chain, policies, objects := moved(t)
		cl := newFakeClient(t, append(objects, chain,
			chainGrant(chainKind, "EventGatewayVirtualCluster"),
			chainGrant(chainKind, policyKind),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked())
		require.Empty(t, cfg.invalidChains)
		require.Len(t, cfg.orderedProducePolicies, 1)
		got := cfg.entities.VirtualClusters[0].ProducePolicies
		require.Len(t, got, 2)
		require.Equal(t, uuid.MustParse(string(policies[1].UID)), got[0].Id, "the declared order, not the default one")
		require.Equal(t, uuid.MustParse(string(policies[0].UID)), got[1].Id)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.Equal(t, other+"/chain/"+string(chain.UID), vc.Annotations[policychain.ProduceOwnerAnnotation])
	})

	t.Run("without a KongReferenceGrant to its policies", func(t *testing.T) {
		gateway, chain, _, objects := moved(t)
		cl := newFakeClient(t, append(objects, chain,
			chainGrant(chainKind, "EventGatewayVirtualCluster"),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NotEmpty(t, cfg.invalidChains, "no other chain owns the virtual cluster: it would keep the default order")
		require.Len(t, cfg.excluded, 1)
		require.Equal(t, eventgatewayv1alpha1.OnPremEntityReferenceNotPermittedReason, cfg.excluded[0].reason)
		require.Contains(t, cfg.excluded[0].message, "no KongReferenceGrant allows it to reference the "+policyKind)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.NotContains(t, vc.Annotations, policychain.ProduceOwnerAnnotation)
	})

	t.Run("without a KongReferenceGrant to its virtual cluster", func(t *testing.T) {
		gateway, chain, _, objects := moved(t)
		cl := newFakeClient(t, append(objects, chain,
			chainGrant(chainKind, policyKind),
		)...)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.NoError(t, cfg.blocked(), "a chain not let reference the virtual cluster doesn't belong to the gateway")
		require.Empty(t, cfg.invalidChains)
		require.Empty(t, cfg.orderedProducePolicies)
		var vc configurationv1alpha1.EventGatewayVirtualCluster
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
		require.NotContains(t, vc.Annotations, policychain.ProduceOwnerAnnotation)
	})
}

// TestBuildConsumePolicyChainOfAnotherNamespace checks a consume chain of
// another namespace than its virtual cluster and its nested policies orders
// them with KongReferenceGrants.
func TestBuildConsumePolicyChainOfAnotherNamespace(t *testing.T) {
	const (
		other     = "other"
		chainKind = "EventGatewayVirtualClusterConsumePolicyChain"
	)
	gateway, chain, policies, objects := consumePolicyChainObjects(t)
	chain.Namespace = other
	chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
	for i := range chain.Spec.APISpec.Policies {
		chain.Spec.APISpec.Policies[i].Namespace = ns
	}
	cl := newFakeClient(t, append(objects, chain,
		chainGrant(chainKind, "EventGatewayVirtualCluster"),
		chainGrant(chainKind, "EventGatewayVirtualClusterConsumePolicy"),
	)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, cfg.blocked())
	require.Empty(t, cfg.invalidChains)
	require.Len(t, cfg.orderedConsumePolicies, 1)

	// The declared order, each child keeping the id of its parent.
	byUID := make(map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, len(policies))
	for _, p := range policies {
		byUID[string(p.UID)] = p
	}
	var got []string
	for _, entity := range cfg.entities.VirtualClusters[0].ConsumePolicies {
		policy := byUID[entity.Id.String()]
		require.NotNil(t, policy)
		got = append(got, policy.Name)
		fields, err := jsonValue(entity)
		require.NoError(t, err)
		parentID, _ := fields.(map[string]any)["parent_policy_id"].(string)
		if parent := policy.Spec.APISpec.ParentPolicyRef.Name; parent != "" {
			require.Equal(t, string(policies[parent].UID), parentID, "the parent of %s", policy.Name)
		} else {
			require.Empty(t, parentID)
		}
	}
	var want []string
	for _, ref := range chain.Spec.APISpec.Policies {
		want = append(want, ref.Name)
	}
	require.Equal(t, want, got)
	var vc configurationv1alpha1.EventGatewayVirtualCluster
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, &vc))
	require.Equal(t, policychain.ClaimValue(chain), vc.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, other+"/"+chain.Name+"/"+string(chain.UID), policychain.ClaimValue(chain), "the claim names the namespace of the chain")
}

// TestBuildConsumePolicyChainWithParentOfAnotherNamespace checks a consume
// chain orders children whose schemaValidation parent is of another
// namespace: the children keep the id of that parent, not of a policy of
// their namespace.
func TestBuildConsumePolicyChainWithParentOfAnotherNamespace(t *testing.T) {
	const (
		other      = "other"
		policyKind = "EventGatewayVirtualClusterConsumePolicy"
	)
	gateway, chain, policies, objects := consumePolicyChainObjects(t)
	parse := policies["parse"]
	parse.Namespace = other
	parse.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns)
	var children []string
	for name, p := range policies {
		if p.Spec.APISpec.ParentPolicyRef.Name == "parse" {
			p.Spec.APISpec.ParentPolicyRef.Namespace = other
			children = append(children, name)
		}
	}
	require.NotEmpty(t, children)
	for i := range chain.Spec.APISpec.Policies {
		if chain.Spec.APISpec.Policies[i].Name == "parse" {
			chain.Spec.APISpec.Policies[i].Namespace = other
		}
	}
	grants := []client.Object{
		// The parent may reference the virtual cluster, the children their
		// parent, and the chain the parent.
		grantIn(ns, other, policyKind, "EventGatewayVirtualCluster"),
		grantIn(other, ns, policyKind, policyKind),
		grantIn(other, ns, "EventGatewayVirtualClusterConsumePolicyChain", policyKind),
	}
	cl := newFakeClient(t, append(append(objects, chain), grants...)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NoError(t, cfg.blocked())
	require.Empty(t, cfg.invalidChains)
	require.Len(t, cfg.orderedConsumePolicies, 1)

	byUID := make(map[string]*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, len(policies))
	for _, p := range policies {
		byUID[string(p.UID)] = p
	}
	var got []string
	for _, entity := range cfg.entities.VirtualClusters[0].ConsumePolicies {
		policy := byUID[entity.Id.String()]
		require.NotNil(t, policy)
		got = append(got, policy.Name)
		fields, err := jsonValue(entity)
		require.NoError(t, err)
		parentID, _ := fields.(map[string]any)["parent_policy_id"].(string)
		if slices.Contains(children, policy.Name) {
			require.Equal(t, string(parse.UID), parentID, "%s keeps the id of its parent of the namespace %s", policy.Name, other)
		}
	}
	var want []string
	for _, ref := range chain.Spec.APISpec.Policies {
		want = append(want, ref.Name)
	}
	require.Equal(t, want, got)

	// Without the grant of the chain to the parent, the chain can't order it.
	cl = newFakeClient(t, append(append(objects, chain), grants[:2]...)...)
	cfg, err = buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.invalidChains)
	require.Contains(t, strings.Join(cfg.invalidChains, "; "), "no KongReferenceGrant allows it to reference the "+policyKind)
}
