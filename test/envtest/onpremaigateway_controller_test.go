package envtest

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/kong/kong-operator/v2/pkg/consts"
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

	// After the manager's cache sync (waited for below), readiness has been
	// observed to take ~16s in CI under -race + parallel envtest load, so keep
	// this window generous - same as assertExpectedEvents in
	// configerrorevent_envtest_test.go. This const also sets the reconciler's
	// CacheSyncTimeout, raising it from the package-default 20s.
	const waitTime = time.Minute

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "onprem-aigw-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:           mgr.GetClient(),
			TypeConverter:    ssaProvider,
			InstancesManager: instancesMgr,
			RestConfig:       cfg,
			Scheme:           scheme.Get(),
			CacheSyncTimeout: waitTime,
			// Used to provision the mTLS client certificate the instances present
			// to their data planes' Admin API when pushing configuration.
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
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

// TestOnPremAIGatewayReconciler_ConfigTracksAIGatewayModels verifies the configuration-change
// pipeline end to end: an AIGatewayModel pointing at an OnPremAIGateway is reconciled by the
// onpremconfig controller, which notifies the shared ChangeNotifier, which wakes the running
// control plane instance so that it re-renders the gateway's configuration document. It also
// verifies that deleting the model notifies the instance with the parent gateway reference
// that the reconciler stored in its cache.
func TestOnPremAIGatewayReconciler_ConfigTracksAIGatewayModels(t *testing.T) {
	t.Parallel()

	// After the manager's cache sync (waited for below), readiness has been
	// observed to take ~16s in CI under -race + parallel envtest load, so keep
	// this window generous - same as assertExpectedEvents in
	// configerrorevent_envtest_test.go.
	const waitTime = time.Minute

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "onprem-aigw-models-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:           mgr.GetClient(),
			TypeConverter:    ssaProvider,
			InstancesManager: instancesMgr,
			RestConfig:       cfg,
			Scheme:           scheme.Get(),
			CacheSyncTimeout: waitTime,
			// Used to provision the mTLS client certificate the instances present
			// to their data planes' Admin API when pushing configuration.
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
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

	// The reconcilers write the Programmed condition on the OnPremAIGateway status and
	// requeue after each status update, so an entity create may trigger more than one
	// reconcile. The log line carries no create/delete distinction, so the deletion
	// check below only counts notifications logged after the delete.
	countNotifications := func(name string, since time.Time) (n int) {
		for _, entry := range logs.All() {
			if entry.Time.After(since) &&
				entry.Message == "Received change notification" &&
				slices.ContainsFunc(entry.Context, func(f zapcore.Field) bool { return f.Key == "name" && f.String == name }) &&
				slices.ContainsFunc(entry.Context, func(f zapcore.Field) bool { return f.Key == "namespace" && f.String == ns.Name }) {
				n++
			}
		}
		return n
	}

	t.Log("Creating the AIGatewayModelProvider the model's target references")
	provider := &aiconfigurationv1alpha1.AIGatewayModelProvider{
		Name:      "test-provider",
		Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayModelProviderSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
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

	// The model's render resolves its provider through the instance's cache. Informers for
	// different kinds are not ordered, so creating the model right away can render it
	// before the provider reaches that cache, failing the render (retried by the instance,
	// but caught by the zero-failures check below). The provider's change notification is
	// sent by a reconciler reading that same cache, so wait for it first.
	t.Log("Expecting the instance to receive the provider's change notification")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Positive(ct, countNotifications(provider.Name, time.Time{}))
	}, waitTime, tickTime)

	t.Log("Creating an AIGatewayModel pointing at the OnPremAIGateway")
	model := &aiconfigurationv1alpha1.AIGatewayModel{
		Name:      "test-model",
		Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group: aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				//nolint:staticcheck
				Type:          aiconfigurationv1alpha1.AIGatewayRefTypeNamespacedRef,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
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

	// The render step runs unobserved: sendConfig only logs on failure. Assert that no
	// render failed, so a broken instance-cache field index or a conversion failure cannot
	// hide behind the notification checks above.
	countSendFailures := func() (n int) {
		for _, entry := range logs.All() {
			if entry.Message == "Failed to send configuration" {
				n++
			}
		}
		return n
	}

	t.Log("Expecting the instance to receive the change notification and re-render its configuration")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Positive(ct, countNotifications(model.Name, time.Time{}))
		assert.Zero(ct, countSendFailures())
	}, waitTime, tickTime)

	t.Log("Deleting the AIGatewayModel")
	deleteStart := time.Now()
	require.NoError(t, cl.Delete(ctx, model))

	t.Log("Expecting the instance to receive the model's deletion notification, addressed to its parent gateway")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Positive(ct, countNotifications(model.Name, deleteStart))
		assert.Zero(ct, countSendFailures())
	}, waitTime, tickTime)
}

// TestOnPremAIGatewayReconciler_PreservesInstanceOwnedCondition verifies that the
// controller's SSA status apply does not steal ownership of the DataPlanesConfigured
// condition from the instance's field manager: applyStatus excludes that condition
// from its payload, so a stale cached status cannot force-re-apply it under the
// controller's field manager.
func TestOnPremAIGatewayReconciler_PreservesInstanceOwnedCondition(t *testing.T) {
	t.Parallel()

	const waitTime = time.Minute

	// The instance's SSA field manager, as defined (unexported) in
	// pkg/multiinstance/aigateway/adminapi_push.go.
	const instanceFieldManager = "gateway-operator-aigateway-instance"

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "onprem-aigw-instance-cond-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:           mgr.GetClient(),
			TypeConverter:    ssaProvider,
			InstancesManager: instancesMgr,
			RestConfig:       cfg,
			Scheme:           scheme.Get(),
			CacheSyncTimeout: waitTime,
			// Used to provision the mTLS client certificate the instances present
			// to their data planes' Admin API when pushing configuration.
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name:      "onprem-aigw-instance-cond",
		Namespace: ns.Name,
	}
	require.NoError(t, cl.Create(ctx, onprem))

	t.Log("Expecting the OnPremAIGateway to become ready")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem))
	}, waitTime, tickTime)

	// Simulate the instance applying the DataPlanesConfigured condition under its own
	// field manager, the same way reportPushStatus does.
	gw := &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: onprem.Namespace,
		Name:      onprem.Name,
	}
	// The GVK is required for the SSA apply: a freshly constructed object has an
	// empty TypeMeta.
	gw.SetGroupVersionKind(aigatewayv1alpha1.GroupVersion.WithKind("OnPremAIGateway"))
	gw.Status.Conditions = []metav1.Condition{
		k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType,
			metav1.ConditionTrue,
			aigatewayv1alpha1.OnPremAIGatewayConfigurationPushSucceededReason,
			"Configuration pushed to 1 Admin API endpoints",
		),
	}
	_, err = controllerpkgssa.ApplyStatusIfChanged(
		ctx, mgr.GetLogger(), cl, ssaProvider, gw, instanceFieldManager,
	)
	require.NoError(t, err)

	// Trigger another controller reconcile so that applyStatus runs on a status
	// that includes the instance-owned condition.
	updateStart := time.Now()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		fresh := &aigatewayv1alpha1.OnPremAIGateway{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), fresh)) {
			return
		}
		if fresh.Annotations == nil {
			fresh.Annotations = map[string]string{}
		}
		fresh.Annotations["test.konghq.com/trigger-reconcile"] = "true"
		assert.NoError(ct, cl.Update(ctx, fresh))
	}, waitTime, tickTime)

	// Wait for at least one controller reconciliation after the update.
	countReconciles := func(since time.Time) (n int) {
		for _, entry := range logs.All() {
			if entry.Time.After(since) &&
				entry.Message == "reconciliation complete for OnPremAIGateway resource" {
				n++
			}
		}
		return n
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Positive(ct, countReconciles(updateStart))
	}, waitTime, tickTime)

	// managerEntryContains returns true when the manager's Apply entry for the
	// status subresource mentions the given string in its owned field paths.
	managerEntryContains := func(obj *aigatewayv1alpha1.OnPremAIGateway, manager, substr string) bool {
		for _, entry := range obj.GetManagedFields() {
			if entry.Manager != manager ||
				entry.Subresource != "status" ||
				entry.Operation != metav1.ManagedFieldsOperationApply ||
				entry.FieldsV1 == nil {
				continue
			}
			if strings.Contains(entry.FieldsV1.GetRawString(), substr) {
				return true
			}
		}
		return false
	}

	t.Log("Expecting the instance-owned condition to survive the controller's status apply")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		fresh := &aigatewayv1alpha1.OnPremAIGateway{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), fresh)) {
			return
		}
		cond := apimeta.FindStatusCondition(
			fresh.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType),
		)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushSucceededReason), cond.Reason)
		assert.True(ct, managerEntryContains(fresh, instanceFieldManager, string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType)),
			"instance field manager must own the DataPlanesConfigured condition")
		assert.False(ct, managerEntryContains(fresh, "gateway-operator", string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType)),
			"controller field manager must not own the DataPlanesConfigured condition")
	}, waitTime, tickTime)
}
