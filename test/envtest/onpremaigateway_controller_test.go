package envtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	aigwonprem "github.com/kong/kong-operator/v2/controller/aigateway/onprem"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// TestOnPremAIGatewayReconciler_BecomesReady verifies that the reconciler
// skeleton flips a freshly created OnPremAIGateway's Ready condition from the
// CRD-defaulted Unknown/Pending to True, with ObservedGeneration tracking the
// resource's generation.
func TestOnPremAIGatewayReconciler_BecomesReady(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:        mgr.GetClient(),
			TypeConverter: ssaProvider,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name:      "onprem-aigw",
		Namespace: ns.Name,
	}
	require.NoError(t, cl.Create(ctx, onprem))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		if !assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem)) {
			return
		}
		cond := apimeta.FindStatusCondition(onprem.Status.Conditions, string(aigatewayv1alpha1.ReadyType))
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, onprem.Generation, cond.ObservedGeneration)
		}
	}, waitTime, tickTime)
}
