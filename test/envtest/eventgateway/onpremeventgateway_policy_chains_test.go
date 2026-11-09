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

func TestOnPremEventGatewayClusterPolicyChain(t *testing.T) {
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
	chain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: z-second}, {name: a-first}]
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
			got := make([]string, 0, len(g.entities.VirtualClusters[0].ClusterPolicies))
			for _, p := range g.entities.VirtualClusters[0].ClusterPolicies {
				got = append(got, p.Id.String())
			}
			assert.Equal(ct, ids, got)
			version = g.version.DataVersion
		}, waitTime, tickTime)
		return version
	}
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	policy := func(name string) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
		return objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, fmt.Sprintf(`
metadata: {name: %s}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: acls
    acls:
      name: %s
      config:
        rules:
        - action: allow
          resourceType: topic
          operations: [{name: read}]
          resourceNames: {type: stat, stat: [{match: orders}]}
`, name, name))
	}
	first, second := policy("a-first"), policy("z-second")
	for _, obj := range []client.Object{first, second} {
		require.NoError(t, cl.Create(ctx, obj))
	}
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	version := expectOrder(string(second.UID), string(first.UID))
	require.Empty(t, chain.GetKonnectID())

	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		slices.Reverse(chain.Spec.APISpec.Policies)
		return cl.Update(ctx, chain)
	}))
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Greater(t, expectOrder(string(first.UID), string(second.UID)), version)

	competitor := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: competitor}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: z-second}, {name: a-first}]
`)
	require.NoError(t, cl.Create(ctx, competitor))
	expectCondition(competitor, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	expectCondition(competitor, "Programmed", metav1.ConditionFalse, "Invalid")
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Delete(ctx, competitor))

	version = expectOrder(string(first.UID), string(second.UID))
	require.NoError(t, cl.Delete(ctx, second))
	expectCondition(chain, "Programmed", metav1.ConditionFalse, "Invalid")
	require.Equal(t, version, expectOrder(string(first.UID), string(second.UID)), "an unresolved chain must not replace the running document")
	require.Eventually(t, func() bool {
		return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(second), second))
	}, waitTime, tickTime)
	previousUID := second.UID
	second = policy("z-second")
	require.NoError(t, cl.Create(ctx, second))
	require.NotEqual(t, previousUID, second.UID)
	expectCondition(chain, "Programmed", metav1.ConditionTrue, "Programmed")
	expectOrder(string(first.UID), string(second.UID))

	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), chain); err != nil {
			return err
		}
		slices.Reverse(chain.Spec.APISpec.Policies)
		return cl.Update(ctx, chain)
	}))
	expectOrder(string(second.UID), string(first.UID))
	require.NoError(t, cl.Delete(ctx, chain))
	expectOrder(string(first.UID), string(second.UID))
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterPolicy{first, second} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(p), p), "chain deletion does not delete policy entities")
	}
}
