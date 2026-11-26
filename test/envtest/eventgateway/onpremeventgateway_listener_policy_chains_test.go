package eventgateway

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestOnPremEventGatewayListenerPolicyChain(t *testing.T) {
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
	vc := objectFromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, ns.Name, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: listener
    addresses: ["0.0.0.0"]
    ports: [19092]
`)
	chain := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicyChain](t, ns.Name, `
metadata: {name: chain}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    policies: [{name: z-second}, {name: a-first}]
`)
	for _, obj := range []client.Object{gateway, backend, vc, listener, chain} {
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
			if !assert.True(ct, ok) || !assert.Len(ct, g.entities.Listeners, 1) {
				return
			}
			got := make([]string, 0, len(g.entities.Listeners[0].Policies))
			for _, p := range g.entities.Listeners[0].Policies {
				got = append(got, p.Id.String())
			}
			assert.Equal(ct, ids, got)
			version = g.version.DataVersion
		}, waitTime, tickTime)
		return version
	}
	// The policies it lists don't exist yet.
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	policy := func(name string) *configurationv1alpha1.EventGatewayListenerPolicy {
		return objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, ns.Name, fmt.Sprintf(`
metadata: {name: %s}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: %s
      config:
        type: portMapping
        portMapping:
          advertisedHost: localhost
          destination: {name: vc}
`, name, name))
	}
	first, second := policy("a-first"), policy("z-second")
	for _, obj := range []client.Object{first, second} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	expectCondition(chain, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner")
	version := expectOrder(string(second.UID), string(first.UID))

	reverse := func() {
		t.Helper()
		require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
				return err
			}
			slices.Reverse(chain.Spec.APISpec.Policies)
			return cl.Update(ctx, chain)
		}))
	}
	reverse()
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Greater(t, expectOrder(string(first.UID), string(second.UID)), version)

	competitor := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicyChain](t, ns.Name, `
metadata: {name: competitor}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    policies: [{name: z-second}, {name: a-first}]
`)
	require.NoError(t, cl.Create(ctx, competitor))
	expectCondition(competitor, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	expectCondition(competitor, "Programmed", metav1.ConditionFalse, "Invalid")
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Delete(ctx, competitor))

	// A chain that no longer lists every policy holds back the configuration:
	// with the chain ordering them the other way than by default, so that
	// a push of the default order shows.
	reverse()
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	version = expectOrder(string(second.UID), string(first.UID))
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		chain.Spec.APISpec.Policies = chain.Spec.APISpec.Policies[:1]
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	expectCondition(gateway, "Programmed", metav1.ConditionFalse, "InvalidPolicyChain")
	require.Equal(t, version, expectOrder(string(second.UID), string(first.UID)), "an unresolved chain must not replace the running document")

	// Deleting it restores the default order.
	require.NoError(t, cl.Delete(ctx, chain))
	expectCondition(gateway, "Programmed", metav1.ConditionTrue, "Programmed")
	expectOrder(string(first.UID), string(second.UID))
	for _, p := range []*configurationv1alpha1.EventGatewayListenerPolicy{first, second} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(p), p), "chain deletion does not delete policy entities")
	}
}
