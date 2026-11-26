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
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
