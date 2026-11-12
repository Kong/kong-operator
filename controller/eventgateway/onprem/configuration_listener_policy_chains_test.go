package onprem

import (
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

// listenerPolicyChainObjects returns a gateway with a listener of two
// policies, and a chain ordering them the other way round than their names.
func listenerPolicyChainObjects(t *testing.T) (
	*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayListenerPolicyChain,
	[]*configurationv1alpha1.EventGatewayListenerPolicy, []client.Object,
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
	vc := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	listener := fromYAML[configurationv1alpha1.EventGatewayListener](t, created, `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: listener
    addresses: ["0.0.0.0"]
    ports: [19092]
`)
	policies := make([]*configurationv1alpha1.EventGatewayListenerPolicy, 0, 2)
	objects := []client.Object{gateway, backend, vc, listener}
	for i, name := range []string{"a-first", "z-second"} {
		policy := fromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, created.Add(time.Duration(i)*time.Minute), `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: `+name+`
      config:
        type: portMapping
        portMapping:
          advertisedHost: localhost
          destination: {name: vc}
`)
		policies = append(policies, policy)
		objects = append(objects, policy)
	}
	chain := fromYAML[configurationv1alpha1.EventGatewayListenerPolicyChain](t, created, `
metadata: {name: chain}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    policies:
    - {name: z-second, kind: EventGatewayListenerPolicy, namespace: default}
    - {name: a-first}
`)
	return gateway, chain, policies, objects
}

func TestBuildListenerPolicyChainOwnership(t *testing.T) {
	gateway, chain, policies, objects := listenerPolicyChainObjects(t)
	cl := newFakeClient(t, append(objects, chain)...)
	build := func() configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		require.Empty(t, cfg.invalidChains)
		return cfg
	}
	policyIDs := func(cfg configuration) []string {
		var ids []string
		for _, p := range cfg.entities.Listeners[0].Policies {
			ids = append(ids, p.Id.String())
		}
		return ids
	}
	listener := &configurationv1alpha1.EventGatewayListener{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "listener"}, listener))

	cfg := build()
	require.Equal(t, []string{string(policies[1].UID), string(policies[0].UID)}, policyIDs(cfg))
	require.Equal(t, "EventGatewayListenerPolicy "+ns+"/z-second", cfg.objects["listeners.0.policies.0"])
	require.Equal(t, "EventGatewayListenerPolicy "+ns+"/a-first", cfg.objects["listeners.0.policies.1"])
	require.Equal(t, []uuid.UUID{uuid.MustParse(string(listener.UID))}, cfg.orderedPolicies)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(listener), listener))
	require.Equal(t, policychain.ClaimValue(chain), listener.Annotations[policychain.ListenerOwnerAnnotation])
	i := slices.IndexFunc(cfg.included, func(s entityStatus) bool { return s.obj.GetName() == chain.Name && s.obj.GetUID() == chain.UID })
	require.NotEqual(t, -1, i, "the chain is in the configuration")
	require.Equal(t, "Owner", cfg.included[i].conditions[0].Reason)
	require.Equal(t, "This resource owns the EventGatewayListener policy chain", cfg.included[i].conditions[0].Message)

	// An older competitor can't take the claim of a live chain.
	competitor := chain.DeepCopy()
	competitor.Name, competitor.UID, competitor.ResourceVersion = "competitor", types.UID(uuid.NewString()), ""
	competitor.CreationTimestamp = metav1.NewTime(chain.CreationTimestamp.Add(-time.Hour))
	slices.Reverse(competitor.Spec.APISpec.Policies)
	require.NoError(t, cl.Create(t.Context(), competitor))
	cfg = build()
	require.Equal(t, []string{string(policies[1].UID), string(policies[0].UID)}, policyIDs(cfg))
	require.Len(t, cfg.excluded, 1)
	require.Contains(t, cfg.excluded[0].message, fmt.Sprintf("EventGatewayListener listener is owned by policy chain %s", chain.Name))
	require.Equal(t, "ConflictingChain", cfg.excluded[0].conditions[0].Reason)
	require.False(t, cfg.excluded[0].blocks, "a competing chain doesn't hold back the configuration")

	// Deleting the owner hands the listener over to the competitor.
	require.NoError(t, cl.Delete(t.Context(), read(t, cl, chain)))
	cfg = build()
	require.Equal(t, []string{string(policies[0].UID), string(policies[1].UID)}, policyIDs(cfg))
	// The competitor's order is also the default one: the listener being
	// ordered tells it is applied.
	require.Equal(t, []uuid.UUID{uuid.MustParse(string(listener.UID))}, cfg.orderedPolicies)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(listener), listener))
	require.Equal(t, policychain.ClaimValue(competitor), listener.Annotations[policychain.ListenerOwnerAnnotation])

	// Without a chain, the claim is released and the policies keep their
	// default order.
	require.NoError(t, cl.Delete(t.Context(), competitor))
	cfg = build()
	require.Empty(t, cfg.orderedPolicies)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(listener), listener))
	require.NotContains(t, listener.Annotations, policychain.ListenerOwnerAnnotation)
	require.Len(t, cfg.entities.Listeners[0].Policies, 2, "deleting chains must never delete policy entities")
}

func TestListenerPolicyOrderValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		change  func(*configurationv1alpha1.EventGatewayListenerPolicyChain, []*configurationv1alpha1.EventGatewayListenerPolicy)
		problem string
	}{
		{name: "declared order"},
		{name: "omitted list", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = nil
		}, problem: "must be supplied explicitly"},
		{name: "partial declaration", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = c.Spec.APISpec.Policies[:1]
		}, problem: "must list every listener policy exactly once; policy a-first is missing"},
		{name: "duplicate", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[1] = c.Spec.APISpec.Policies[0]
		}, problem: "duplicates policy"},
		{name: "missing", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Name = "missing"
		}, problem: "references EventGatewayListenerPolicy " + ns + "/missing, which does not exist"},
		{name: "wrong kind", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterPolicy"
		}, problem: "must name an EventGatewayListenerPolicy"},
		{name: "cross namespace policy", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ []*configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, problem: "EventGatewayListenerPolicy other/z-second uses cross-namespace reference from namespace " + ns + ", which is not supported yet"},
		{name: "wrong parent", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p []*configurationv1alpha1.EventGatewayListenerPolicy) {
			p[1].Spec.EventGatewayListenerRef = namespacedRef("other-listener")
		}, problem: "different listener"},
		{name: "terminating policy", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p []*configurationv1alpha1.EventGatewayListenerPolicy) {
			p[1].DeletionTimestamp = new(metav1.Now())
			p[1].Finalizers = []string{"test.example/finalizer"}
		}, problem: "being deleted"},
		{name: "excluded policy", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p []*configurationv1alpha1.EventGatewayListenerPolicy) {
			p[1].UID = types.UID("not-a-uuid")
		}, problem: "not in the valid configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := listenerPolicyChainObjects(t)
			if tt.change != nil {
				tt.change(chain, policies)
			}
			cl := newFakeClient(t, objects...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			ordered, problem, err := policyOrder(t.Context(), &cfg, cl, chain, listenerPolicyChains)
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
		})
	}
}

// TestBuildListenerPolicyChainInvalidHoldsBackConfiguration checks that the
// chain owning a listener, which can't order its policies, holds back the
// configuration rather than letting them be pushed in their default order.
func TestBuildListenerPolicyChainInvalidHoldsBackConfiguration(t *testing.T) {
	gateway, chain, _, objects := listenerPolicyChainObjects(t)
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:1]
	cl := newFakeClient(t, append(objects, chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Len(t, cfg.invalidChains, 1)
	require.Contains(t, cfg.invalidChains[0], ns+"/chain: spec.apiSpec.policies must list every listener policy exactly once")
	require.Empty(t, cfg.orderedPolicies)
	require.Contains(t, invalidPolicyChainsError{problems: cfg.invalidChains}.Error(), "Policy chains prevent applying the complete gateway configuration")
}

// TestBuildListenerPolicyChainOfAnotherNamespace checks a listener policy
// chain of another namespace than its listener is left out, without holding
// back the configuration nor claiming the listener: without a
// KongReferenceGrant as any entity, and with one as it must be in the
// namespace of its listener.
func TestBuildListenerPolicyChainOfAnotherNamespace(t *testing.T) {
	for _, tt := range []struct {
		name    string
		granted bool
		message string
	}{
		{name: "without a KongReferenceGrant", message: "no KongReferenceGrant allows it to reference the EventGatewayListener " + ns + "/listener"},
		{name: "with a KongReferenceGrant", granted: true,
			message: "policy-chain ownership requires a same-namespace parent reference"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway, chain, policies, objects := listenerPolicyChainObjects(t)
			chain.Namespace = "other"
			chain.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new(ns)
			objects = append(objects, chain)
			if tt.granted {
				objects = append(objects, &configurationv1alpha1.KongReferenceGrant{
					Name: "chains", Namespace: ns,
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayListenerPolicyChain", Namespace: "other"}},
						To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: "EventGatewayListener"}},
					},
				})
			}
			cl := newFakeClient(t, objects...)
			cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
			require.NoError(t, err)
			require.Empty(t, cfg.invalidChains)
			require.Empty(t, cfg.orderedPolicies)
			require.Len(t, cfg.excluded, 1)
			require.Equal(t, tt.message, cfg.excluded[0].message)
			require.False(t, cfg.excluded[0].blocks)
			require.Equal(t, policies[0].UID, types.UID(cfg.entities.Listeners[0].Policies[0].Id.String()))
			listener := &configurationv1alpha1.EventGatewayListener{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "listener"}, listener))
			require.NotContains(t, listener.Annotations, policychain.ListenerOwnerAnnotation)
		})
	}
}

// TestBuildListenerPolicyChainWithPolicyOfAnotherNamespace checks a listener
// policy chain is left out, without claiming its listener nor holding back
// the configuration, when the listener has a policy of another namespace a
// KongReferenceGrant lets in the configuration: the chain can't list it.
func TestBuildListenerPolicyChainWithPolicyOfAnotherNamespace(t *testing.T) {
	gateway, chain, policies, objects := listenerPolicyChainObjects(t)
	foreign := policies[0].DeepCopy()
	foreign.Name, foreign.Namespace, foreign.UID = "foreign", "other", types.UID(uuid.NewString())
	foreign.Spec.APISpec.ForwardToVirtualClust.Name = "foreign"
	foreign.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new(ns)
	// Its destination, a virtual cluster of its namespace, of the backend
	// cluster of the gateway.
	foreignVC := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, foreign.CreationTimestamp.Time, `
metadata: {name: vc, namespace: other}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend, namespace: `+ns+`}}
  apiSpec:
    name: vc-other
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc-other
`)
	foreignVC.Namespace = "other"
	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "other", Namespace: ns,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{Group: "configuration.konghq.com", Kind: "EventGatewayListenerPolicy", Namespace: "other"},
				{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualCluster", Namespace: "other"},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{Group: "configuration.konghq.com", Kind: "EventGatewayListener"},
				{Group: "configuration.konghq.com", Kind: "EventGatewayBackendCluster"},
			},
		},
	}
	cl := newFakeClient(t, append(objects, chain, foreign, foreignVC, grant)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.invalidChains, "the chain doesn't hold back the configuration")
	require.Empty(t, cfg.orderedPolicies)
	require.Len(t, cfg.entities.Listeners[0].Policies, 3, "the policy of another namespace is in the configuration")
	require.Len(t, cfg.excluded, 1)
	require.Equal(t, "the EventGatewayListener listener has the EventGatewayListenerPolicy other/foreign of another namespace, which a chain can't order; cross-namespace references are not supported",
		cfg.excluded[0].message)
	listener := &configurationv1alpha1.EventGatewayListener{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "listener"}, listener))
	require.NotContains(t, listener.Annotations, policychain.ListenerOwnerAnnotation)
}

// TestBuildListenerPolicyChainExcludedParentOwnership checks, for a listener
// left out of the configuration, that only its live UID-bound owner holds
// back the configuration, and that inactive claims are released without a
// new one being acquired.
func TestBuildListenerPolicyChainExcludedParentOwnership(t *testing.T) {
	type chain = configurationv1alpha1.EventGatewayListenerPolicyChain
	for _, tt := range []struct {
		name        string
		claim       func(*chain) string
		terminating bool
		blocked     bool
	}{
		{name: "unclaimed"},
		{name: "live owner and competitor", claim: policychain.ClaimValue[*chain], blocked: true},
		{name: "recreated owner", claim: func(c *chain) string { return c.Name + "/" + uuid.NewString() }},
		{name: "missing owner", claim: func(*chain) string { return "missing/" + uuid.NewString() }},
		{name: "malformed claim", claim: func(*chain) string { return "malformed" }},
		{name: "terminating owner", claim: policychain.ClaimValue[*chain], terminating: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, owner, _, objects := listenerPolicyChainObjects(t)
			parent := objects[3].(*configurationv1alpha1.EventGatewayListener)
			if tt.claim != nil {
				parent.Annotations = map[string]string{policychain.ListenerOwnerAnnotation: tt.claim(owner)}
			}
			competitor := owner.DeepCopy()
			competitor.Name, competitor.UID = "competitor", types.UID(uuid.NewString())
			if tt.terminating {
				owner.DeletionTimestamp = new(metav1.Now())
				owner.Finalizers = []string{"test.example/finalizer"}
			}
			// An older listener of the same name leaves the listener out, which
			// holds back the configuration by itself: unlike for a virtual
			// cluster, whether the gateway pushes doesn't tell about its chain.
			validParent := parent.DeepCopy()
			validParent.Name, validParent.UID = "valid-listener", types.UID(uuid.NewString())
			validParent.Annotations = nil
			validParent.CreationTimestamp = metav1.NewTime(parent.CreationTimestamp.Add(-time.Hour))
			f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
				return append(append(f.gwAdmin.Objects(), f.gateway), append(objects[1:], validParent, owner, competitor)...)
			})
			cl := f.cl
			cfg, err := buildConfiguration(t.Context(), cl, f.gateway, "", nil)
			require.NoError(t, err)
			require.Len(t, cfg.entities.Listeners, 1)
			require.Equal(t, string(validParent.UID), cfg.entities.Listeners[0].Id.String())
			require.Empty(t, cfg.orderedPolicies)
			if tt.blocked {
				require.Equal(t, []string{client.ObjectKeyFromObject(owner).String() + ": its EventGatewayListener " +
					client.ObjectKeyFromObject(parent).String() + " isn't in the configuration"}, cfg.invalidChains,
					"only the UID-bound owner may block, not its competitor")
				require.Equal(t, policychain.ClaimValue(owner), read(t, cl, parent).Annotations[policychain.ListenerOwnerAnnotation])
			} else {
				require.Empty(t, cfg.invalidChains)
				require.NotContains(t, read(t, cl, parent).Annotations, policychain.ListenerOwnerAnnotation,
					"an excluded parent must not acquire a new claim after releasing an inactive owner")
			}
			_, err = f.r.Reconcile(t.Context(), read(t, cl, f.gateway))
			require.NoError(t, err)
			condition := apimeta.FindStatusCondition(read(t, cl, competitor).Status.Conditions, "Programmed")
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Contains(t, condition.Message, "isn't in the configuration")
		})
	}
}

// TestBuildListenerPolicyChainOfListenerNotPermitted checks the claim on a
// listener of another namespace no KongReferenceGrant lets reference the
// gateway isn't the gateway's to release, even a stale one, and that its
// chain doesn't hold back the configuration.
func TestBuildListenerPolicyChainOfListenerNotPermitted(t *testing.T) {
	gateway, chain, _, objects := listenerPolicyChainObjects(t)
	listener := objects[3].(*configurationv1alpha1.EventGatewayListener)
	listener.Namespace = "other"
	listener.Spec.ParentRef.Namespace = new(ns)
	stale := "missing/" + uuid.NewString()
	listener.Annotations = map[string]string{policychain.ListenerOwnerAnnotation: stale}
	chain.Namespace = "other"
	cl := newFakeClient(t, append(objects, chain)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.entities.Listeners)
	require.Empty(t, cfg.invalidChains)
	require.Equal(t, stale, read(t, cl, listener).Annotations[policychain.ListenerOwnerAnnotation])
}

// TestReconcileListenerPolicyChainLifecycle checks, through the reconciler,
// that the order a chain declares is pushed, repaired when gw-admin holds
// another one, frozen while the chain is invalid, and back to the default
// one once the chain is deleted, without pushing an unchanged configuration.
func TestReconcileListenerPolicyChainLifecycle(t *testing.T) {
	_, chain, policies, objects := listenerPolicyChainObjects(t)
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	for _, obj := range append(objects[1:], chain) {
		require.NoError(t, f.cl.Create(t.Context(), obj))
	}
	reconcile := func() {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
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
		held := pod.gateways[f.gatewayID].Listeners
		require.Len(t, held, 1)
		ids := make([]string, 0, len(held[0].Policies))
		for _, policy := range held[0].Policies {
			ids = append(ids, policy.Id.String())
		}
		return ids
	}
	expectProgrammed := func(want metav1.ConditionStatus, reason string) {
		t.Helper()
		cond := apimeta.FindStatusCondition(read(t, f.cl, chain).Status.Conditions, "Programmed")
		require.NotNil(t, cond)
		require.Equal(t, want, cond.Status, cond.Message)
		require.Equal(t, reason, cond.Reason, cond.Message)
	}
	declared := []string{string(policies[1].UID), string(policies[0].UID)}
	byDefault := []string{string(policies[0].UID), string(policies[1].UID)}

	reconcile()
	require.Equal(t, declared, heldIDs())
	expectProgrammed(metav1.ConditionTrue, "Programmed")
	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "an unchanged ordered configuration must not be pushed again")
	pushed := read(t, f.cl, f.gateway)

	f.pod().mu.Lock()
	remote := f.pod().gateways[f.gatewayID]
	slices.Reverse(remote.Listeners[0].Policies)
	f.pod().gateways[f.gatewayID] = remote
	f.pod().mu.Unlock()
	f.pod().reset()
	reconcile()
	require.Contains(t, f.pod().recorded(), callEntities, "a changed remote order is repaired even at the same revision")
	require.Equal(t, pushed.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Equal(t, declared, heldIDs())

	chain = read(t, f.cl, chain)
	chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:1]
	chain.Generation++
	require.NoError(t, f.cl.Update(t.Context(), chain))
	f.pod().reset()
	reconcile()
	require.Empty(t, f.pod().recorded(), "an incomplete owner freezes the whole document, including its revision")
	require.Equal(t, pushed.Status.ConfigRevision, read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Equal(t, declared, heldIDs())
	expectProgrammed(metav1.ConditionFalse, "Invalid")
	require.Equal(t, "InvalidPolicyChain", apimeta.FindStatusCondition(read(t, f.cl, f.gateway).Status.Conditions, "Programmed").Reason)

	require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, chain)))
	reconcile()
	require.Equal(t, byDefault, heldIDs(), "deletion restores the creation/name default order")
	f.pod().reset()
	reconcile()
	require.NotContains(t, f.pod().recorded(), callEntities, "no-chain configurations must not be pushed repeatedly")
}
