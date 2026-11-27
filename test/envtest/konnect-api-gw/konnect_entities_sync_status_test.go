package konnectapigw

import (
	"context"
	"sync"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// fakeKonnectServiceConnector emulates the Konnect Service API for the sync state tests.
type fakeKonnectServiceConnector struct {
	mu      sync.Mutex
	stored  sdkkonnectcomp.Service
	gets    int
	upserts int
}

func (f *fakeKonnectServiceConnector) expect(t *testing.T, sdk *sdkmocks.MockSDKWrapper) {
	t.Helper()

	sdk.ServicesSDK.EXPECT().UpsertService(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertServiceRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertServiceResponse, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.upserts++
			f.stored = sdkkonnectcomp.Service{
				ID:        new(req.ServiceID),
				Name:      req.ServiceRequest.ServiceWithHost.Name,
				Host:      req.ServiceRequest.ServiceWithHost.Host,
				UpdatedAt: new(int64(f.upserts)),
			}
			return &sdkkonnectops.UpsertServiceResponse{Service: new(f.stored)}, nil
		},
	).Maybe()

	sdk.ServicesSDK.EXPECT().GetService(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, sdkkonnectops.GetServiceRequest, ...sdkkonnectops.Option) (*sdkkonnectops.GetServiceResponse, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.gets++
			stored := f.stored
			return &sdkkonnectops.GetServiceResponse{Service: new(stored)}, nil
		},
	).Maybe()
}

func (f *fakeKonnectServiceConnector) setHost(host string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored.Host = host
}

func (f *fakeKonnectServiceConnector) calls() (gets, upserts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.upserts
}

// TestKongServiceSyncStatusPersistsAcrossRestart verifies that a new tracker
// (as after a restart or leader change) uses the persisted status hashes to
// verify an unchanged entity with a GET instead of sending a PUT, and still
// sends a PUT when the representation drifted.
func TestKongServiceSyncStatusPersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	seed := client.NewNamespacedClient(envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name)

	const (
		serviceID = "service-sync-status"
		host      = "example.com"
	)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, seed)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, seed, apiAuth)
	svc := deploy.KongService(t, ctx, seed,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		func(obj client.Object) {
			obj.(*configurationv1alpha1.KongService).Spec.Host = host
		},
	)
	envtest.UpdateKongServiceStatusWithProgrammed(t, ctx, seed, svc, serviceID, cp.GetKonnectID())

	factory := sdkmocks.NewMockSDKFactory(t)
	connector := &fakeKonnectServiceConnector{}
	connector.expect(t, factory.SDK)
	factory.SDK.DPNodesSDK.EXPECT().
		GetExpectedConfigVersion(mock.Anything, cp.GetKonnectID()).
		Return(&sdkkonnectops.GetExpectedConfigVersionResponse{
			GetExpectedConfigVersionResponse: &sdkkonnectcomp.GetExpectedConfigVersionResponse{
				ExpectedConfigVersion: "1",
			},
		}, nil).
		Maybe()

	startManager := func(t *testing.T, tracker *ops.ConfigVersionTracker) {
		t.Helper()
		mgrCtx, mgrCancel := context.WithCancel(ctx)
		mgr, logs := envtest.NewManager(t, mgrCtx, cfg, scheme.Get())
		envtest.StartReconcilers(mgrCtx, t, mgr, logs,
			konnect.NewKonnectEntityReconciler(
				factory, logging.DevelopmentMode, mgr.GetClient(),
				konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongService](0),
				konnect.WithMetricRecorder[configurationv1alpha1.KongService](&metricsmocks.MockRecorder{}),
				konnect.WithConfigVersionTracker[configurationv1alpha1.KongService](tracker),
			),
		)
		// Registered after StartReconcilers so it runs first and lets the
		// manager's cleanup wait for a clean shutdown.
		t.Cleanup(mgrCancel)
	}

	waitForSyncStatus := func(t *testing.T) *configurationv1alpha1.KongService {
		t.Helper()
		var got configurationv1alpha1.KongService
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			if !assert.NoError(c, seed.Get(ctx, k8stypes.NamespacedName{Name: svc.Name, Namespace: ns.Name}, &got)) {
				return
			}
			assert.NotNil(c, got.GetSyncStatus(), "sync status must be persisted after a PUT")
		}, consts.WaitTime, consts.TickTime)
		return &got
	}

	t.Run("first reconcile sends and persists the sync state", func(t *testing.T) {
		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		waitForSyncStatus(t)
		_, upserts := connector.calls()
		assert.GreaterOrEqual(t, upserts, 1)
	})

	t.Run("restart with matching representation verifies without a PUT", func(t *testing.T) {
		getsBefore, upsertsBefore := connector.calls()

		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			gets, _ := connector.calls()
			assert.Greater(c, gets, getsBefore, "the restarted tracker must GET-verify the entity")
		}, consts.WaitTime, consts.TickTime)
		gets, upserts := connector.calls()
		assert.Greater(t, gets, getsBefore)
		assert.Equal(t, upsertsBefore, upserts, "no PUT must be sent when the representation matches")

		persisted := waitForSyncStatus(t)
		assert.NotNil(t, persisted.GetSyncStatus())
	})

	t.Run("restart with a drifted representation sends a PUT", func(t *testing.T) {
		getsBefore, upsertsBefore := connector.calls()
		connector.setHost("changed.example.com")

		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			gets, upserts := connector.calls()
			assert.Greater(c, gets, getsBefore, "the restarted tracker must GET-verify the entity")
			assert.Greater(c, upserts, upsertsBefore, "a drifted representation must be sent")
		}, consts.WaitTime, consts.TickTime)

		// The PUT records the new representation, which matches the desired state again.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var got configurationv1alpha1.KongService
			if !assert.NoError(c, seed.Get(ctx, k8stypes.NamespacedName{Name: svc.Name, Namespace: ns.Name}, &got)) {
				return
			}
			assert.NotNil(c, got.GetSyncStatus())
			assert.Equal(c, host, connectorHost(t, connector))
		}, consts.WaitTime, consts.TickTime)
	})
}

func connectorHost(t *testing.T, connector *fakeKonnectServiceConnector) string {
	t.Helper()
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return connector.stored.Host
}

// fakeKonnectVaultConnector emulates the Konnect Vault API for the sync state tests.
type fakeKonnectVaultConnector struct {
	mu      sync.Mutex
	stored  sdkkonnectcomp.Vault
	gets    int
	upserts int
}

func (f *fakeKonnectVaultConnector) expect(t *testing.T, sdk *sdkmocks.MockSDKWrapper) {
	t.Helper()

	sdk.VaultSDK.EXPECT().UpsertVault(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertVaultRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertVaultResponse, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.upserts++
			f.stored = req.Vault
			f.stored.ID = new(req.VaultID)
			f.stored.UpdatedAt = new(int64(f.upserts))
			stored := f.stored
			return &sdkkonnectops.UpsertVaultResponse{Vault: &stored}, nil
		},
	).Maybe()

	sdk.VaultSDK.EXPECT().GetVault(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, sdkkonnectops.GetVaultRequest, ...sdkkonnectops.Option) (*sdkkonnectops.GetVaultResponse, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.gets++
			stored := f.stored
			return &sdkkonnectops.GetVaultResponse{Vault: &stored}, nil
		},
	).Maybe()
}

func (f *fakeKonnectVaultConnector) setDescription(description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored.Description = new(description)
}

func (f *fakeKonnectVaultConnector) description() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stored.Description == nil {
		return ""
	}
	return *f.stored.Description
}

func (f *fakeKonnectVaultConnector) calls() (gets, upserts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.upserts
}

// TestKongVaultSyncStatusPersistsAcrossRestart verifies that KongVault persists
// sync status in its CRD status and uses it across restarts to GET-verify
// unchanged vaults instead of sending a PUT, while still sending a PUT on drift.
func TestKongVaultSyncStatusPersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	clusterClient := envtest.NewControllerClient(t, scheme.Get(), cfg)
	seed := client.NewNamespacedClient(clusterClient, ns.Name)

	const (
		vaultID     = "vault-sync-status"
		description = "initial-description"
	)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, seed)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, seed, apiAuth)
	_ = deploy.KongReferenceGrant(t, ctx, seed,
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
			Kind:      "KongVault",
			Namespace: configurationv1alpha1.Namespace(""),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:  "KonnectGatewayControlPlane",
		}),
	)

	vault := deploy.KongVaultAttachedToCP(t, ctx, clusterClient, "env", "env-sync-status", []byte(`{"prefix":"env_sync_"}`), cp,
		func(obj client.Object) {
			obj.(*configurationv1alpha1.KongVault).Spec.Description = description
		},
	)
	vault.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
		ControlPlaneID: cp.GetKonnectID(),
		ID:             vaultID,
	}
	vault.Status.Conditions = []metav1.Condition{
		envtest.ProgrammedCondition(vault.GetGeneration()),
	}
	require.NoError(t, clusterClient.Status().Update(ctx, vault))

	factory := sdkmocks.NewMockSDKFactory(t)
	connector := &fakeKonnectVaultConnector{}
	connector.expect(t, factory.SDK)
	factory.SDK.DPNodesSDK.EXPECT().
		GetExpectedConfigVersion(mock.Anything, cp.GetKonnectID()).
		Return(&sdkkonnectops.GetExpectedConfigVersionResponse{
			GetExpectedConfigVersionResponse: &sdkkonnectcomp.GetExpectedConfigVersionResponse{
				ExpectedConfigVersion: "1",
			},
		}, nil).
		Maybe()

	startManager := func(t *testing.T, tracker *ops.ConfigVersionTracker) {
		t.Helper()
		mgrCtx, mgrCancel := context.WithCancel(ctx)
		mgr, logs := envtest.NewManager(t, mgrCtx, cfg, scheme.Get())
		envtest.StartReconcilers(mgrCtx, t, mgr, logs,
			konnect.NewKonnectEntityReconciler(
				factory, logging.DevelopmentMode, mgr.GetClient(),
				konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongVault](0),
				konnect.WithMetricRecorder[configurationv1alpha1.KongVault](&metricsmocks.MockRecorder{}),
				konnect.WithConfigVersionTracker[configurationv1alpha1.KongVault](tracker),
			),
		)
		t.Cleanup(mgrCancel)
	}

	waitForSyncStatus := func(t *testing.T) *configurationv1alpha1.KongVault {
		t.Helper()
		var got configurationv1alpha1.KongVault
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			if !assert.NoError(c, clusterClient.Get(ctx, k8stypes.NamespacedName{Name: vault.Name}, &got)) {
				return
			}
			assert.NotNil(c, got.GetSyncStatus(), "sync status must be persisted after a PUT")
		}, consts.WaitTime, consts.TickTime)
		return &got
	}

	t.Run("first reconcile sends and persists the sync state", func(t *testing.T) {
		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		waitForSyncStatus(t)
		_, upserts := connector.calls()
		assert.GreaterOrEqual(t, upserts, 1)
	})

	t.Run("restart with matching representation verifies without a PUT", func(t *testing.T) {
		getsBefore, upsertsBefore := connector.calls()

		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			gets, _ := connector.calls()
			assert.Greater(c, gets, getsBefore, "the restarted tracker must GET-verify the vault")
		}, consts.WaitTime, consts.TickTime)
		gets, upserts := connector.calls()
		assert.Greater(t, gets, getsBefore)
		assert.Equal(t, upsertsBefore, upserts, "no PUT must be sent when the representation matches")

		persisted := waitForSyncStatus(t)
		assert.NotNil(t, persisted.GetSyncStatus())
	})

	t.Run("restart with a drifted representation sends a PUT", func(t *testing.T) {
		getsBefore, upsertsBefore := connector.calls()
		connector.setDescription("drifted-description")

		startManager(t, ops.NewConfigVersionTracker(consts.KonnectInfiniteSyncTime, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			gets, upserts := connector.calls()
			assert.Greater(c, gets, getsBefore, "the restarted tracker must GET-verify the vault")
			assert.Greater(c, upserts, upsertsBefore, "a drifted representation must be sent")
		}, consts.WaitTime, consts.TickTime)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			var got configurationv1alpha1.KongVault
			if !assert.NoError(c, clusterClient.Get(ctx, k8stypes.NamespacedName{Name: vault.Name}, &got)) {
				return
			}
			assert.NotNil(c, got.GetSyncStatus())
			assert.Equal(c, description, connector.description())
		}, consts.WaitTime, consts.TickTime)
	})
}
