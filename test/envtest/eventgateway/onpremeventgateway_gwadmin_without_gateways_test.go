package eventgateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestOnPremEventGatewayProvisionsGWAdminWithoutGateways verifies that the
// gw-admin pods of a GWAdmin no OnPremEventGateway uses are told they serve
// no gateway, so that they turn Ready and the rollouts of the GWAdmin don't
// stall, a restarted one too, and that a gateway takes over once it uses the
// GWAdmin, from its namespace or, with a KongReferenceGrant, from another.
func TestOnPremEventGatewayProvisionsGWAdminWithoutGateways(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))

	// expectServing waits for the pods to be told they serve the gateways.
	expectServing := func(t *testing.T, pods []*fakeGWAdmin, gateways func(*assert.CollectT) []uuid.UUID) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			want := gateways(ct)
			for _, pod := range pods {
				_, _, expected := pod.state(uuid.Nil)
				if assert.NotNil(ct, expected, "the pod must be told the gateways it serves") {
					assert.ElementsMatch(ct, want, expected)
				}
			}
		}, waitTime, tickTime)
	}
	none := func(*assert.CollectT) []uuid.UUID { return []uuid.UUID{} }

	pod0 := gwAdmin.provision(ctx)

	t.Run("the pods of a GWAdmin without gateways are told they serve none", func(t *testing.T) {
		expectServing(t, []*fakeGWAdmin{pod0}, none)
	})

	t.Run("so is a new pod, e.g. one of a rollout", func(t *testing.T) {
		pod1 := gwAdmin.startPod(ctx, "gw-admin-1", "10.0.0.2")
		expectServing(t, []*fakeGWAdmin{pod0, pod1}, none)
	})

	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)

	t.Run("a gateway using the GWAdmin takes over", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, gateway))
		expectServing(t, []*fakeGWAdmin{pod0}, func(ct *assert.CollectT) []uuid.UUID {
			return []uuid.UUID{gatewayID(ct, gateway)}
		})
	})

	t.Run("once the gateway is gone, a new pod is told it serves none", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gateway))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			err := cl.Get(ctx, client.ObjectKeyFromObject(gateway), &eventgatewayv1alpha1.OnPremEventGateway{})
			assert.True(ct, apierrors.IsNotFound(err), "the gateway must be gone: %v", err)
		}, waitTime, tickTime)
		pod2 := gwAdmin.startPod(ctx, "gw-admin-2", "10.0.0.3")
		expectServing(t, []*fakeGWAdmin{pod0, pod2}, none)
	})

	t.Run("so is a restarted pod, which keeps nothing", func(t *testing.T) {
		gwAdmin.setServing(ctx, "10.0.0.3", true)
		gwAdmin.stopPod(ctx, "10.0.0.3")
		restarted := gwAdmin.startPod(ctx, "gw-admin-2", "10.0.0.3")
		expectServing(t, []*fakeGWAdmin{restarted}, none)
	})

	t.Run("a gateway of another namespace takes over once a KongReferenceGrant allows it", func(t *testing.T) {
		other := envtest.CreateNamespace(ctx, t, cl)
		crossNamespace := &eventgatewayv1alpha1.OnPremEventGateway{
			Name: "gateway", Namespace: other.Name,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: testGWAdminName, Namespace: new(ns.Name)}},
		}
		require.NoError(t, cl.Create(ctx, crossNamespace))
		pod3 := gwAdmin.startPod(ctx, "gw-admin-3", "10.0.0.4")
		expectServing(t, []*fakeGWAdmin{pod0, pod3}, none)

		grant := gwadminreftest.Grant(ns.Name, "gateways-of-"+other.Name, other.Name, "")
		require.NoError(t, cl.Create(ctx, grant))
		expectServing(t, []*fakeGWAdmin{pod0, pod3}, func(ct *assert.CollectT) []uuid.UUID {
			return []uuid.UUID{gatewayID(ct, crossNamespace)}
		})

		// The grant removed leaves the GWAdmin without gateways.
		require.NoError(t, cl.Delete(ctx, grant))
		expectServing(t, []*fakeGWAdmin{pod0, pod3}, none)
		pod4 := gwAdmin.startPod(ctx, "gw-admin-4", "10.0.0.5")
		expectServing(t, []*fakeGWAdmin{pod4}, none)
	})
}
