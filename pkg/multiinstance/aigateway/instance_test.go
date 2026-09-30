package aigateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

func adminAPI(address string) adminapi.DiscoveredAdminAPI {
	return adminapi.DiscoveredAdminAPI{
		Address:       address,
		TLSServerName: "pod.dp-admin.default.svc",
		PodRef:        types.NamespacedName{Namespace: "default", Name: "dp"},
	}
}

// waitForNotification returns the next Change from the notifier channel, or nil when
// no notification arrives.
func waitForNotification(ch <-chan changenotifier.Change) *changenotifier.Change {
	select {
	case change := <-ch:
		return &change
	default:
		return nil
	}
}

func TestOnAdminAPIsDiscovered(t *testing.T) {
	ctx := context.Background()
	gatewayNN := types.NamespacedName{Namespace: "default", Name: "gw"}

	instance := NewInstance(
		manager.NewRandomID(),
		logr.Discard(),
		Config{},
		Env{GatewayNN: gatewayNN},
	)
	ch := instance.cn.NotifyChannel()

	oneEndpoint := sets.New(adminAPI("https://10.0.0.1:8444"))
	otherEndpoint := sets.New(adminAPI("https://10.0.0.2:8444"))

	// Discovery from nothing to one endpoint notifies the sync loop.
	instance.onAdminAPIsDiscovered(ctx, oneEndpoint)
	change := waitForNotification(ch)
	require.NotNil(t, change, "expected a change notification for the first discovery")
	require.Equal(t, gatewayNN, *change.ParentNN)

	// Unchanged set stays silent but is stored.
	instance.onAdminAPIsDiscovered(ctx, oneEndpoint)
	require.Nil(t, waitForNotification(ch), "unchanged set must not notify")
	require.True(t, instance.AdminAPIs().Equal(oneEndpoint))

	// Set emptied stays silent but the empty set is stored.
	instance.onAdminAPIsDiscovered(ctx, sets.New[adminapi.DiscoveredAdminAPI]())
	require.Nil(t, waitForNotification(ch), "emptied set must not notify")
	require.Empty(t, instance.AdminAPIs())

	// Refilled set notifies again.
	instance.onAdminAPIsDiscovered(ctx, otherEndpoint)
	change = waitForNotification(ch)
	require.NotNil(t, change, "expected a change notification for the refilled set")
	require.Equal(t, gatewayNN, *change.ParentNN)
	require.True(t, instance.AdminAPIs().Equal(otherEndpoint))
}

// TestHash verifies that the instance config hash covers both the rendered payload
// and the client certificate Secret reference, and that an empty config keeps the
// historical sha256("") value (the reconciler compares it against the running
// instance's hash to detect drift, e.g. after a certificate renewal re-creates the
// Secret under a new name).
func TestHash(t *testing.T) {
	// sha256 of empty input, the value produced before AdminClientCertSecretNN
	// was added to Config.
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	hashOf := func(cfg Config) string {
		h, err := Hash(cfg)
		require.NoError(t, err)
		return h
	}

	require.Equal(t, emptySHA256, hashOf(Config{}))

	base := Config{
		DBLessConfig:            []byte("payload"),
		AdminClientCertSecretNN: types.NamespacedName{Namespace: "default", Name: "cert-1"},
	}
	require.Equal(t, hashOf(base), hashOf(Config{
		DBLessConfig:            base.DBLessConfig,
		AdminClientCertSecretNN: base.AdminClientCertSecretNN,
	}), "identical configs must produce identical hashes")

	require.NotEqual(t, hashOf(base), hashOf(Config{
		DBLessConfig:            base.DBLessConfig,
		AdminClientCertSecretNN: types.NamespacedName{Namespace: "default", Name: "cert-2"},
	}), "a renewed Secret under a new name must drift the hash")
}

// testAIGatewayModel returns a valid AIGatewayModel pointing at the test gateway.
func testAIGatewayModel(name string) *aiconfigurationv1alpha1.AIGatewayModel {
	return &aiconfigurationv1alpha1.AIGatewayModel{
		Name: name, Namespace: testGatewayNamespace,
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: testGatewayName},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
						Formats:     []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{Paths: []string{"/" + name}},
						},
					},
				},
			},
		},
	}
}

// TestSendConfig drives the sendConfig glue between translation, rendering, push and
// per-entity status reporting: it asserts what the EntityStatusReporter holds after each
// branch, not just the returned error (the pieces each have their own tests, the glue
// is what was missing).
func TestSendConfig(t *testing.T) {
	ctx := context.Background()
	gwNN := types.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}

	statusOf := func(instance *Instance, obj client.Object) status.ConfigurationStatus {
		return instance.statusReporter.KubernetesObjectConfigurationStatus(obj)
	}
	messageOf := func(instance *Instance, obj client.Object) string {
		return instance.statusReporter.KubernetesObjectConfigurationStatusMessage(obj)
	}

	t.Run("reports per-entity translation failures and succeeds for the rest", func(t *testing.T) {
		ok := testAIGatewayModel("model-a")
		broken := testAIGatewayModel("model-broken")
		broken.Spec.APISpec = aiconfigurationv1alpha1.AIGatewayModelAPISpec{}
		instance := testPushInstance(t, adminClientCertSecret(), ok, broken)
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444")))

		require.NoError(t, instance.sendConfig(ctx, &gwNN))

		require.Equal(t, status.ConfigurationStatusSucceeded, statusOf(instance, ok))
		require.True(t, instance.statusReporter.KubernetesObjectIsConfigured(ok))
		require.Empty(t, messageOf(instance, ok))

		require.Equal(t, status.ConfigurationStatusFailed, statusOf(instance, broken))
		require.Contains(t, messageOf(instance, broken), "spec.apiSpec is required")

		// The payload was still pushed for the surviving entity.
		require.Len(t, factory.built, 1)
		require.Len(t, factory.clients["https://10.0.0.1:8444"].payloads, 1)
	})

	t.Run("reports all entities as failed when the push fails", func(t *testing.T) {
		modelA := testAIGatewayModel("model-a")
		modelB := testAIGatewayModel("model-b")
		instance := testPushInstance(t, adminClientCertSecret(), modelA, modelB)
		factory := &fakePushClientFactory{
			failures: map[string]error{"https://10.0.0.1:8444": errors.New("connection refused")},
		}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444")))

		err := instance.sendConfig(ctx, &gwNN)
		require.Error(t, err)
		require.Contains(t, err.Error(), "sending configuration to data planes")

		for _, m := range []*aiconfigurationv1alpha1.AIGatewayModel{modelA, modelB} {
			require.Equal(t, status.ConfigurationStatusFailed, statusOf(instance, m))
			require.Contains(t, messageOf(instance, m), "connection refused")
		}
	})

	t.Run("reports all entities as failed when no endpoints are discovered", func(t *testing.T) {
		model := testAIGatewayModel("model-a")
		instance := testPushInstance(t, adminClientCertSecret(), model)
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New[adminapi.DiscoveredAdminAPI]())

		// The skip is not an error: syncPending drops the gateway from pending on it.
		require.NoError(t, instance.sendConfig(ctx, &gwNN))
		require.Empty(t, factory.built)

		require.Equal(t, status.ConfigurationStatusFailed, statusOf(instance, model))
		require.Contains(t, messageOf(instance, model), "no Admin API endpoints discovered")
	})

	t.Run("reports success for all entities", func(t *testing.T) {
		modelA := testAIGatewayModel("model-a")
		modelB := testAIGatewayModel("model-b")
		instance := testPushInstance(t, adminClientCertSecret(), modelA, modelB)
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444")))

		require.NoError(t, instance.sendConfig(ctx, &gwNN))

		for _, m := range []*aiconfigurationv1alpha1.AIGatewayModel{modelA, modelB} {
			require.Equal(t, status.ConfigurationStatusSucceeded, statusOf(instance, m))
			require.Empty(t, messageOf(instance, m))
		}
	})

	t.Run("the skip path lets syncPending drop the gateway from pending", func(t *testing.T) {
		model := testAIGatewayModel("model-a")
		instance := testPushInstance(t, adminClientCertSecret(), model)
		instance.setAdminAPIs(sets.New[adminapi.DiscoveredAdminAPI]())

		pending := map[types.NamespacedName]struct{}{gwNN: {}}
		var lastSyncTS time.Time
		instance.syncPending(ctx, pending, &lastSyncTS)

		require.Empty(t, pending, "the skipped sync must still drop the gateway from pending")
		require.Equal(t, status.ConfigurationStatusFailed, statusOf(instance, model))
	})
}
