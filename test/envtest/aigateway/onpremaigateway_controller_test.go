package aigateway

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	aigwonprem "github.com/kong/kong-operator/v2/controller/aigateway/onprem"
	"github.com/kong/kong-operator/v2/controller/crdschema"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/create"
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
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "onprem-aigw-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	envtest.StartReconcilers(ctx, t, mgr, logs,
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

// TestOnPremAIGatewayReconciler_ConfigTracksEntities verifies the configuration-change
// pipeline end to end: an AIGatewayModel pointing at an OnPremAIGateway is reconciled by the
// onpremconfig controller, which notifies the shared ChangeNotifier, which wakes the running
// control plane instance so that it re-renders the gateway's configuration document. It also
// verifies that deleting the model notifies the instance with the parent gateway reference
// that the reconciler stored in its cache, and that a change to an AIGatewayConsumerCredential
// - whose rendered form lives in the parent consumer's document, so that the consumer must
// re-render it - is mapped to its parent consumer and notifies the instance through it.
func TestOnPremAIGatewayReconciler_ConfigTracksEntities(t *testing.T) {
	t.Parallel()

	// After the manager's cache sync (waited for below), readiness has been
	// observed to take ~16s in CI under -race + parallel envtest load, so keep
	// this window generous - same as assertExpectedEvents in
	// configerrorevent_envtest_test.go.
	const waitTime = time.Minute

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "onprem-aigw-models-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	envtest.StartReconcilers(ctx, t, mgr, logs,
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

	t.Log("Creating an AIGatewayConsumer for the credential checks below")
	consumer := onPremConsumer("test-consumer", ns.Name, onprem.Name)
	require.NoError(t, cl.Create(ctx, consumer))

	countConsumerNotifications := func() int {
		return countNotifications(consumer.Name, time.Time{})
	}
	// The consumer is created here, well before the credential checks, so that the burst of
	// its own notifications - its reconcile notifies, and the instance's status report
	// re-triggers it through the status queue - has died down by then.
	t.Log("Expecting the instance to receive the consumer's change notification")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Positive(ct, countConsumerNotifications())
	}, waitTime, tickTime)

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

	// The credential's own controller only writes its Programmed condition and never feeds
	// the ChangeNotifier: its rendered form lives in the consumer's document, so the consumer
	// is what the credential's watch must re-reconcile. The
	// consumer's own notifications must have stopped by now, otherwise they cannot be told
	// apart from the credential's. The consumer's burst is a chain of reconciles, each
	// waiting on a sync or a status update round-trip, so it can outlast the model steps
	// above: wait for the count to hold steady rather than assuming it already does.
	waitConsumerNotificationsSettled := func(msg string) (settled int) {
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			settled = countConsumerNotifications()
			time.Sleep(3 * time.Second)
			assert.Equal(ct, settled, countConsumerNotifications(), msg)
		}, waitTime, tickTime)
		return settled
	}
	settled := waitConsumerNotificationsSettled("the consumer's own notifications must go quiet before the credential is created")

	t.Log("Creating an AIGatewayConsumerCredential for the consumer")
	credential := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Name:      "test-credential",
		Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: consumer.Name},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				Name:        "test-credential",
				DisplayName: "Test Credential",
				Type:        "api-key",
				APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new("test-api-key"),
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, credential))

	t.Log("Expecting the credential's creation to notify the instance through its parent consumer")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Greater(ct, countConsumerNotifications(), settled)
		assert.Zero(ct, countSendFailures())
	}, waitTime, tickTime)

	settled = waitConsumerNotificationsSettled("the consumer's notifications must go quiet before the credential is updated")

	t.Log("Updating the credential's display name")
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(credential), credential))
	credential.Spec.APISpec.DisplayName = "Updated Credential"
	require.NoError(t, cl.Update(ctx, credential))

	// The update touches neither the consumer nor the credential's parent reference, so only
	// the credential's own event can be what notifies the instance here.
	t.Log("Expecting the credential's update to notify the instance through its parent consumer")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Greater(ct, countConsumerNotifications(), settled)
		assert.Zero(ct, countSendFailures())
	}, waitTime, tickTime)

	// The credential reports its own configuration status: its outcome is reported by the
	// consumer's translation and written onto the credential by its own reconciler, so it is
	// no longer only visible on the consumer. This gateway has no data plane, so the push is
	// skipped and the credential is reported as failed, like every other entity here.
	t.Log("Expecting the credential's own Programmed condition to be reported")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(credential), credential)) {
			return
		}
		cond := apimeta.FindStatusCondition(credential.Status.Conditions, string(configurationv1.ConditionProgrammed))
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		assert.Equal(ct, credential.Generation, cond.ObservedGeneration)
	}, waitTime, tickTime)
}

// onPremConsumer returns an AIGatewayConsumer in namespace ns targeting the OnPremAIGateway
// gwName, with the APISpec fields the CRD requires.
func onPremConsumer(name, ns, gwName string) *aiconfigurationv1alpha1.AIGatewayConsumer {
	return &aiconfigurationv1alpha1.AIGatewayConsumer{
		Name:      name,
		Namespace: ns,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gwName},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerAPISpec{
				Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				DisplayName: name,
				Type:        "api-key",
			},
		},
	}
}

// TestOnPremAIGatewayReconciler_PreservesInstanceOwnedCondition verifies that the
// controller's SSA status apply does not steal ownership of the conditions the
// instance owns (DataPlanesConfigured and ConfigurationTranslated) from the
// instance's field manager: applyStatus excludes those conditions from its
// payload, so a stale cached status cannot force-re-apply them under the
// controller's field manager.
func TestOnPremAIGatewayReconciler_PreservesInstanceOwnedCondition(t *testing.T) {
	t.Parallel()

	const waitTime = time.Minute

	// The instance's SSA field managers, as defined (unexported) in
	// pkg/multiinstance/aigateway/adminapi_push.go: one per condition type,
	// exactly like instanceFieldManagerFor builds them.
	const instanceFieldManagerPrefix = "gateway-operator-aigateway-instance"
	instanceFieldManagerFor := func(condType string) string {
		return instanceFieldManagerPrefix + "-" + condType
	}

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "onprem-aigw-instance-cond-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	envtest.StartReconcilers(ctx, t, mgr, logs,
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

	// Simulate the instance applying each of its conditions under its own
	// per-condition-type field manager, the same way reportPushStatus and
	// reportTranslationStatus do through applyInstanceConditions.
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
		ctx, mgr.GetLogger(), cl, ssaProvider, gw,
		instanceFieldManagerFor(string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType)),
	)
	require.NoError(t, err)
	gw.Status.Conditions = []metav1.Condition{
		k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedType,
			metav1.ConditionTrue,
			aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedReason,
			"Configuration translated successfully",
		),
	}
	_, err = controllerpkgssa.ApplyStatusIfChanged(
		ctx, mgr.GetLogger(), cl, ssaProvider, gw,
		instanceFieldManagerFor(string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedType)),
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

	t.Log("Expecting the instance-owned conditions to survive the controller's status apply")
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

		translationCond := apimeta.FindStatusCondition(
			fresh.Status.Conditions, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedType),
		)
		if !assert.NotNil(ct, translationCond, "the ConfigurationTranslated condition must not be pruned") {
			return
		}
		assert.Equal(ct, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedReason), translationCond.Reason)

		for _, condType := range []string{
			string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType),
			string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedType),
		} {
			// Each condition is owned by its own per-condition-type manager, and
			// only by it.
			assert.True(ct, managerEntryContains(fresh, instanceFieldManagerFor(condType), condType),
				"instance field manager for %s must own the %s condition", condType, condType)
			assert.False(ct, managerEntryContains(fresh, instanceFieldManagerPrefix, condType),
				"the bare instance field manager prefix must not own the %s condition", condType)
			assert.False(ct, managerEntryContains(fresh, "gateway-operator", condType),
				"controller field manager must not own the %s condition", condType)
		}
	}, waitTime, tickTime)
}

// TestOnPremAIGatewayReconciler_ObjectFilters verifies that with
// spec.objectFilters.secrets.matchLabels set:
//   - the gateway still becomes ready and reports OptionsValid=True,
//   - the operator-provisioned Admin API client certificate Secret is stamped
//     with the user's matchLabels, so it stays visible in the instance's
//     matchLabels-filtered Secret cache.
func TestOnPremAIGatewayReconciler_ObjectFilters(t *testing.T) {
	t.Parallel()

	const waitTime = time.Minute

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "onprem-aigw-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	envtest.StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:                   mgr.GetClient(),
			TypeConverter:            ssaProvider,
			InstancesManager:         instancesMgr,
			RestConfig:               cfg,
			Scheme:                   scheme.Get(),
			CacheSyncTimeout:         waitTime,
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
			SecretLabelSelector:      "app",
		},
		&crdschema.Reconciler{
			Client:   mgr.GetClient(),
			Provider: ssaProvider,
		},
	)

	cl := mgr.GetClient()

	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name:      "onprem-aigw-filters",
		Namespace: ns.Name,
		Spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
			ObjectFilters: &aigatewayv1alpha1.OnPremAIGatewayObjectFilters{
				Secrets: &aigatewayv1alpha1.OnPremAIGatewayFilterForObjectType{
					MatchLabels: map[string]string{"team": "foo"},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, onprem))

	t.Log("Expecting the OnPremAIGateway to become ready with OptionsValid=True")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		if !assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem)) {
			return
		}
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.OnPremAIGatewayOptionsValidType, onprem))
	}, waitTime, tickTime)

	t.Log("Expecting the Admin API client certificate Secret to carry the objectFilters matchLabels")
	var secrets corev1.SecretList
	require.NoError(t, cl.List(ctx, &secrets, client.InNamespace(ns.Name),
		client.MatchingLabels{consts.SecretOnPremAIGatewayAdminClientCertificateLabel: "true"}))
	require.Len(t, secrets.Items, 1)
	certSecret := secrets.Items[0]
	require.Equal(t, "true", certSecret.Labels["app"], "the operator's secret label selector must be stamped")
	require.Equal(t, "foo", certSecret.Labels["team"], "the objectFilters matchLabels must be stamped")
}

// TestOnPremAIGatewayReconciler_WatchNamespacesRequireGrant verifies the multi-namespace
// gate: a gateway that lists another namespace in spec.watchNamespaces does not schedule
// its instance before that namespace grants it permission, and the WatchNamespaceGrant
// watch re-triggers the reconcile that schedules the instance once the grant exists.
func TestOnPremAIGatewayReconciler_WatchNamespacesRequireGrant(t *testing.T) {
	t.Parallel()

	// Same generous window as the sibling tests: readiness takes ~16s in CI under
	// -race + parallel envtest load.
	const waitTime = time.Minute

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "onprem-aigw-watch-ns-cluster-ca")

	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, aigwCRDGroups)
	require.NoError(t, err)

	instancesMgr := multiinstanceai.NewManager(mgr.GetLogger())
	require.NoError(t, mgr.Add(instancesMgr))

	envtest.StartReconcilers(ctx, t, mgr, logs,
		&aigwonprem.Reconciler{
			Client:                   mgr.GetClient(),
			TypeConverter:            ssaProvider,
			InstancesManager:         instancesMgr,
			RestConfig:               cfg,
			Scheme:                   scheme.Get(),
			CacheSyncTimeout:         waitTime,
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

	watchedNS := &corev1.Namespace{Name: ns.Name + "-watched"}
	require.NoError(t, cl.Create(ctx, watchedNS))

	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name: "onprem-aigw-watch-ns", Namespace: ns.Name,
		Spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
			WatchNamespaces: &aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{
				Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeList,
				List: []string{watchedNS.Name},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, onprem))

	mgrID, err := manager.NewID(string(onprem.GetUID()))
	require.NoError(t, err)

	t.Log("Expecting WatchNamespaceGrantValid=False and no control plane instance")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		cond := apimeta.FindStatusCondition(
			onprem.Status.Conditions,
			string(aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidType),
		)
		if assert.NotNil(ct, cond) {
			assert.Equal(ct, metav1.ConditionFalse, cond.Status)
		}
	}, waitTime, tickTime)
	require.ErrorIs(t, instancesMgr.IsInstanceReady(mgrID), instances.NewInstanceNotFoundError(mgrID))

	t.Log("Creating the WatchNamespaceGrant in the watched namespace")
	grant := &operatorv1alpha1.WatchNamespaceGrant{
		Name: "grant", Namespace: watchedNS.Name,
		Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
			From: []operatorv1alpha1.WatchNamespaceGrantFrom{{
				Group:     aigatewayv1alpha1.GroupName,
				Kind:      "OnPremAIGateway",
				Namespace: ns.Name,
			}},
		},
	}
	require.NoError(t, cl.Create(ctx, grant))

	t.Log("Expecting the grant condition and Ready to become true")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidType, onprem))
		assert.True(ct, k8sutils.HasConditionTrue(aigatewayv1alpha1.ReadyType, onprem))
	}, waitTime, tickTime)

	t.Log("Revoking the grant")
	require.NoError(t, cl.Delete(ctx, grant))

	t.Log("Expecting Ready=False and the control plane instance to be stopped")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(onprem), onprem)) {
			return
		}
		grantCond := apimeta.FindStatusCondition(
			onprem.Status.Conditions,
			string(aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidType),
		)
		if assert.NotNil(ct, grantCond) {
			assert.Equal(ct, metav1.ConditionFalse, grantCond.Status)
		}
		readyCond := apimeta.FindStatusCondition(
			onprem.Status.Conditions,
			string(aigatewayv1alpha1.ReadyType),
		)
		if assert.NotNil(ct, readyCond) {
			assert.Equal(ct, metav1.ConditionFalse, readyCond.Status)
		}
	}, waitTime, tickTime)
	// StopInstance only closes the stop channel; the registry drops the entry from its run
	// goroutine, so poll for the removal instead of racing it.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.ErrorIs(ct, instancesMgr.IsInstanceReady(mgrID), instances.NewInstanceNotFoundError(mgrID))
	}, waitTime, tickTime)
}
