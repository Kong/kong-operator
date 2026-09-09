package envtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	aigwonprem "github.com/kong/kong-operator/v2/controller/aigateway/onprem"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/multiinstanceai"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// TestOnPremAIGatewayReconciler_BecomesReady verifies that the reconciler
// flips a freshly created OnPremAIGateway's Ready condition from the
// CRD-defaulted Unknown/Pending to True, with ObservedGeneration tracking the
// resource's generation, and that it schedules and tears down the control plane
// instance backing the resource.
func TestOnPremAIGatewayReconciler_BecomesReady(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:           mgr.GetClient(),
			TypeConverter:    ssaProvider,
			InstancesManager: instancesMgr,
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

	mgrID, err := manager.NewID(string(onprem.GetUID()))
	require.NoError(t, err)

	t.Log("Expecting the OnPremAIGateway to become ready")
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

	t.Log("Expecting the control plane instance to be scheduled and its config hash reported in the status")
	hash, err := instancesMgr.GetInstanceConfigHash(mgrID)
	require.NoError(t, err)
	require.Equal(t, hash, onprem.Status.ConfigHash)

	t.Log("Deleting the OnPremAIGateway")
	require.NoError(t, cl.Delete(ctx, onprem))

	t.Log("Expecting the control plane instance to be torn down and the resource to be gone")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := instancesMgr.IsInstanceReady(mgrID)
		assert.ErrorIs(ct, err, instances.NewInstanceNotFoundError(mgrID))
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)))
	}, waitTime, tickTime)
}
