package envtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	aigwonprem "github.com/kong/kong-operator/v2/controller/aigateway/onprem"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
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

// TestOnPremAIGatewayReconciler_ConfigTracksAIGatewayModels verifies that an AIGatewayModel
// pointing at an OnPremAIGateway drives its rendered configuration: creating one changes
// status.configHash, exercising the index registration, the watch on AIGatewayModel, and the
// drift-detection loop together.
func TestOnPremAIGatewayReconciler_ConfigTracksAIGatewayModels(t *testing.T) {
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
		Name:      "onprem-aigw-models",
		Namespace: ns.Name,
	}
	require.NoError(t, cl.Create(ctx, onprem))

	t.Log("Expecting the OnPremAIGateway to become ready with no AIGatewayModels")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem))
	}, waitTime, tickTime)
	hashBefore := onprem.Status.ConfigHash
	require.NotEmpty(t, hashBefore)

	t.Log("Creating the AIGatewayModelProvider the model's target references")
	provider := &aiconfigurationv1alpha1.AIGatewayModelProvider{
		Name:      "test-provider",
		Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayModelProviderSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: onprem.Name},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelProviderAPISpec{
				AIGatewayModelProviderConfig: &aiconfigurationv1alpha1.AIGatewayModelProviderConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelProviderConfigTypeOpenai,
					Openai: &aiconfigurationv1alpha1.AIGatewayModelProviderOpenai{
						Name:        "test-provider",
						DisplayName: "Test Provider",
						Config: aiconfigurationv1alpha1.AIGatewayModelProviderOpenaiConfig{
							Auth: aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasic{
								Headers: []aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasicHeaders{
									{Name: "Authorization"},
								},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, provider))

	t.Log("Creating an AIGatewayModel pointing at the OnPremAIGateway")
	model := &aiconfigurationv1alpha1.AIGatewayModel{
		Name:      "test-model",
		Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: onprem.Name},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:         "test-model",
						DisplayName:  "Test Model",
						Formats:      []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Capabilities: []string{"generate"},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{Paths: []string{"/test-model"}},
						},
						Targets: []aiconfigurationv1alpha1.AIGatewayTarget{
							{
								Name:     "test-model",
								Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{Name: "test-provider"},
								Config:   &aiconfigurationv1alpha1.AIGatewayTargetConfig{Type: aiconfigurationv1alpha1.AIGatewayTargetConfigTypeOpenai},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, model))

	t.Log("Expecting the rendered configuration hash to change")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		assert.NotEqual(ct, hashBefore, onprem.Status.ConfigHash)
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem))
	}, waitTime, tickTime)
}
