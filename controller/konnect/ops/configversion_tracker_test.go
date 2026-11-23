package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const (
	testCPID             = "cp-1"
	testVersionTTL       = time.Minute
	testDriftCheckPeriod = time.Hour
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func newTestConfigVersionTracker() (*ConfigVersionTracker, *testClock) {
	clock := &testClock{now: time.Unix(1_000_000, 0)}
	tracker := NewConfigVersionTracker(testVersionTTL, testDriftCheckPeriod)
	tracker.now = clock.Now
	return tracker, clock
}

type fakeConfigVersion struct {
	sdk     *mocks.MockDPNodesSDK
	version string
	err     error
	calls   int
	// afterRead, when set, runs after the version to return was read, simulating concurrent activity.
	afterRead func()
}

func newFakeConfigVersion(t *testing.T) *fakeConfigVersion {
	f := &fakeConfigVersion{sdk: mocks.NewMockDPNodesSDK(t), version: "1"}
	f.sdk.EXPECT().GetExpectedConfigVersion(mock.Anything, testCPID).RunAndReturn(
		func(ctx context.Context, _ string, _ ...sdkkonnectops.Option) (*sdkkonnectops.GetExpectedConfigVersionResponse, error) {
			f.calls++
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if f.err != nil {
				return nil, f.err
			}
			version := f.version
			if afterRead := f.afterRead; afterRead != nil {
				f.afterRead = nil
				afterRead()
			}
			return &sdkkonnectops.GetExpectedConfigVersionResponse{
				GetExpectedConfigVersionResponse: &sdkkonnectcomp.GetExpectedConfigVersionResponse{
					ExpectedConfigVersion: version,
				},
			}, nil
		},
	).Maybe()
	return f
}

// fakeKonnectServices stores the service last upserted and returns it on get, with an updated_at
// timestamp that changes on every upsert.
type fakeKonnectServices struct {
	sdk       *mocks.MockServicesSDK
	stored    *sdkkonnectcomp.ServiceOutput
	getErr    error
	updatedAt int64
	gets      int
	upserts   int
}

func newFakeKonnectServices(t *testing.T) *fakeKonnectServices {
	f := &fakeKonnectServices{sdk: mocks.NewMockServicesSDK(t)}
	f.sdk.EXPECT().UpsertService(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertServiceRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertServiceResponse, error) {
			f.upserts++
			f.updatedAt++
			f.stored = &sdkkonnectcomp.ServiceOutput{
				ID:        new(req.ServiceID),
				Name:      req.Service.Name,
				Host:      req.Service.Host,
				UpdatedAt: new(f.updatedAt),
			}
			return &sdkkonnectops.UpsertServiceResponse{Service: new(*f.stored)}, nil
		},
	).Maybe()
	f.sdk.EXPECT().GetService(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetServiceResponse, error) {
			f.gets++
			if f.getErr != nil {
				return nil, f.getErr
			}
			return &sdkkonnectops.GetServiceResponse{Service: new(*f.stored)}, nil
		},
	).Maybe()
	return f
}

func (f *fakeKonnectServices) calls() (gets, upserts int) {
	gets, upserts = f.gets, f.upserts
	f.gets, f.upserts = 0, 0
	return gets, upserts
}

func newTestKongService() *configurationv1alpha1.KongService {
	svc := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "default",
			UID:       k8stypes.UID("svc-uid"),
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: new("svc"),
				Host: "example.com",
			},
		},
		Status: configurationv1alpha1.KongServiceStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
				ControlPlaneID: testCPID,
			},
		},
	}
	svc.SetKonnectID("svc-konnect-id")
	return svc
}

func TestConfigVersionTrackerUpdateService(t *testing.T) {
	type env struct {
		tracker  *ConfigVersionTracker
		clock    *testClock
		version  *fakeConfigVersion
		services *fakeKonnectServices
		svc      *configurationv1alpha1.KongService
	}
	setup := func(t *testing.T) env {
		tracker, clock := newTestConfigVersionTracker()
		e := env{
			tracker:  tracker,
			clock:    clock,
			version:  newFakeConfigVersion(t),
			services: newFakeKonnectServices(t),
			svc:      newTestKongService(),
		}
		return e
	}
	update := func(t *testing.T, e env, periodic bool) syncOutcome {
		t.Helper()
		g := e.tracker.guard(e.version.sdk, e.svc.GetUID(), testCPID, periodic)
		require.NoError(t, updateService(t.Context(), e.services.sdk, g, e.svc))
		return g.outcome
	}
	synced := func(t *testing.T) env {
		t.Helper()
		e := setup(t)
		require.Equal(t, syncOutcomeSent, update(t, e, true))
		e.services.calls()
		e.version.calls = 0
		return e
	}

	t.Run("first update sends after fetching the config version baseline", func(t *testing.T) {
		e := setup(t)

		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 0, gets)
		assert.Equal(t, 1, upserts)
		assert.Equal(t, 1, e.version.calls)
	})

	t.Run("unchanged periodic update is skipped", func(t *testing.T) {
		e := synced(t)

		for range 3 {
			e.clock.Advance(testVersionTTL)
			assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
		}
		gets, upserts := e.services.calls()
		assert.Equal(t, 0, gets)
		assert.Equal(t, 0, upserts)
		assert.Equal(t, 3, e.version.calls)
	})

	t.Run("config version is fetched at most once per TTL", func(t *testing.T) {
		e := synced(t)

		for range 5 {
			update(t, e, true)
		}
		assert.Equal(t, 0, e.version.calls)
	})

	t.Run("payload change sends", func(t *testing.T) {
		e := synced(t)

		e.svc.Spec.Name = new("renamed")
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		_, upserts := e.services.calls()
		assert.Equal(t, 1, upserts)

		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
	})

	t.Run("non periodic update always sends", func(t *testing.T) {
		e := synced(t)

		assert.Equal(t, syncOutcomeSent, update(t, e, false))
		_, upserts := e.services.calls()
		assert.Equal(t, 1, upserts)
	})

	t.Run("external change matching the last update is verified with a get", func(t *testing.T) {
		e := synced(t)

		e.version.version = "2"
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)

		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
		gets, _ = e.services.calls()
		assert.Equal(t, 0, gets)
	})

	t.Run("external change of the entity sends", func(t *testing.T) {
		e := synced(t)

		e.services.stored.Host = "changed.example.com"
		e.version.version = "2"
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 1, upserts)
		assert.Equal(t, "example.com", e.services.stored.Host)
	})

	t.Run("failure to get the entity sends", func(t *testing.T) {
		e := synced(t)

		e.services.getErr = errors.New("not found")
		e.version.version = "2"
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		_, upserts := e.services.calls()
		assert.Equal(t, 1, upserts)
	})

	operatorWrite := func(t *testing.T, e env, version string) {
		t.Helper()
		e.tracker.BeforeWrite(t.Context(), testCPID)
		e.version.version = version
		e.tracker.AfterWrite(t.Context(), testCPID)
		e.tracker.waitForRebaseline()
	}

	t.Run("config version change caused by operator writes does not trigger a get", func(t *testing.T) {
		e := synced(t)

		operatorWrite(t, e, "2")
		assert.Equal(t, 2, e.version.calls, "config version must be read before and after the write")
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 0, gets)
		assert.Equal(t, 0, upserts)

		e.version.version = "3"
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
	})

	t.Run("external change before an operator write triggers a get", func(t *testing.T) {
		e := synced(t)

		e.version.version = "2"
		operatorWrite(t, e, "3")
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, _ := e.services.calls()
		assert.Equal(t, 1, gets)
	})

	t.Run("external change after an operator write triggers a get", func(t *testing.T) {
		e := synced(t)

		operatorWrite(t, e, "2")
		e.version.version = "3"
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, _ := e.services.calls()
		assert.Equal(t, 1, gets)
	})

	t.Run("overlapping operator writes read the config version once before and once after", func(t *testing.T) {
		e := synced(t)

		e.tracker.BeforeWrite(t.Context(), testCPID)
		e.tracker.BeforeWrite(t.Context(), testCPID)
		e.version.version = "2"
		e.tracker.AfterWrite(t.Context(), testCPID)
		e.version.version = "3"
		e.tracker.AfterWrite(t.Context(), testCPID)
		e.tracker.waitForRebaseline()
		assert.Equal(t, 2, e.version.calls)

		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
	})

	t.Run("operator write starting while the config version is read after a write is attributed to the operator",
		func(t *testing.T) {
			e := synced(t)

			e.tracker.BeforeWrite(t.Context(), testCPID)
			e.version.version = "2"
			e.version.afterRead = func() {
				e.tracker.BeforeWrite(t.Context(), testCPID)
				e.version.version = "3"
			}
			e.tracker.AfterWrite(t.Context(), testCPID)
			e.tracker.waitForRebaseline()
			e.tracker.AfterWrite(t.Context(), testCPID)
			e.tracker.waitForRebaseline()
			assert.Equal(t, 3, e.version.calls)

			e.clock.Advance(testVersionTTL)
			assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
		},
	)

	t.Run("operator write before any config version baseline does not read it", func(t *testing.T) {
		e := setup(t)

		operatorWrite(t, e, "2")
		assert.Equal(t, 0, e.version.calls)
	})

	t.Run("write without a baseline does not mask a later external change", func(t *testing.T) {
		e := synced(t)

		// Simulate losing the control plane baseline (for example after a
		// server URL change) while the entity sync record is still present.
		e.tracker.mu.Lock()
		e.tracker.controlPlanes[testCPID].version = ""
		e.tracker.mu.Unlock()

		e.version.version = "1"
		e.tracker.BeforeWrite(t.Context(), testCPID)
		e.version.version = "2"
		e.tracker.AfterWrite(t.Context(), testCPID)
		e.tracker.waitForRebaseline()

		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
	})

	t.Run("unchanged periodic update is skipped between drift checks", func(t *testing.T) {
		e := synced(t)

		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 0, gets)
		assert.Equal(t, 0, upserts)
		assert.Equal(t, 1, e.version.calls)
	})

	t.Run("drift check verifies an unchanged entity without a version change", func(t *testing.T) {
		e := synced(t)

		e.clock.Advance(testDriftCheckPeriod + testDriftCheckPeriod/10 + time.Millisecond)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
	})

	t.Run("failed pre-write config version read does not mask an external change", func(t *testing.T) {
		e := synced(t)

		e.version.version = "2"
		e.version.err = errors.New("transient")
		e.tracker.BeforeWrite(t.Context(), testCPID)

		e.version.err = nil
		e.version.version = "3"
		e.tracker.AfterWrite(t.Context(), testCPID)
		e.tracker.waitForRebaseline()

		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
	})

	t.Run("canceled write context does not poison the config version state", func(t *testing.T) {
		e := synced(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		e.tracker.BeforeWrite(ctx, testCPID)
		e.tracker.AfterWrite(ctx, testCPID)
		e.tracker.waitForRebaseline()

		cp := e.tracker.controlPlanes[testCPID]
		assert.False(t, cp.fetchErr)
		assert.Equal(t, "1", cp.version)
	})

	t.Run("failure to get the config version sends and is not retried within TTL", func(t *testing.T) {
		e := synced(t)

		e.version.err = errors.New("forbidden")
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		_, upserts := e.services.calls()
		assert.Equal(t, 2, upserts)
		assert.Equal(t, 1, e.version.calls)
	})

	t.Run("failure of the first config version fetch sends and is not retried within TTL", func(t *testing.T) {
		e := setup(t)

		e.version.err = errors.New("forbidden")
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
		_, upserts := e.services.calls()
		assert.Equal(t, 2, upserts)
		assert.Equal(t, 1, e.version.calls)

		e.version.err = nil
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSkipped, update(t, e, true))
	})

	t.Run("config version change after a fetch failure triggers a get", func(t *testing.T) {
		e := synced(t)

		e.version.err = errors.New("unavailable")
		e.clock.Advance(testVersionTTL)
		assert.Equal(t, syncOutcomeSent, update(t, e, true))

		e.version.err = nil
		e.version.version = "2"
		e.clock.Advance(testVersionTTL)
		e.services.calls()
		assert.Equal(t, syncOutcomeVerified, update(t, e, true))
		gets, upserts := e.services.calls()
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
	})

	t.Run("forgotten entity is sent", func(t *testing.T) {
		e := synced(t)

		e.tracker.Forget(e.svc.GetUID())
		assert.Equal(t, syncOutcomeSent, update(t, e, true))
	})

	t.Run("failed upsert is not recorded", func(t *testing.T) {
		tracker, _ := newTestConfigVersionTracker()
		version := newFakeConfigVersion(t)
		sdk := mocks.NewMockServicesSDK(t)
		svc := newTestKongService()
		sdk.EXPECT().UpsertService(mock.Anything, mock.Anything).Return(nil, errors.New("boom")).Once()

		g := tracker.guard(version.sdk, svc.GetUID(), testCPID, true)
		require.Error(t, updateService(t.Context(), sdk, g, svc))
		_, ok := tracker.record(svc.GetUID())
		assert.False(t, ok)
	})
}

func TestConfigVersionTrackerNilGuardAlwaysSends(t *testing.T) {
	services := newFakeKonnectServices(t)
	svc := newTestKongService()

	var tracker *ConfigVersionTracker
	assert.NotPanics(t, func() {
		tracker.BeforeWrite(t.Context(), testCPID)
		tracker.AfterWrite(t.Context(), testCPID)
	})
	for range 2 {
		g := tracker.guard(nil, svc.GetUID(), testCPID, true)
		require.NoError(t, updateService(t.Context(), services.sdk, g, svc))
	}
	_, upserts := services.calls()
	assert.Equal(t, 2, upserts)
}

func TestConfigVersionTrackerNilSDKGuardAlwaysSends(t *testing.T) {
	tracker, _ := newTestConfigVersionTracker()
	services := newFakeKonnectServices(t)
	svc := newTestKongService()

	g := tracker.guard(nil, svc.GetUID(), testCPID, true)
	require.Nil(t, g)
	require.NoError(t, updateService(t.Context(), services.sdk, g, svc))
	_, upserts := services.calls()
	assert.Equal(t, 1, upserts)
}

func TestConfigVersionTrackerConsumerGroupGetIsWrapped(t *testing.T) {
	newGroup := func() *configurationv1beta1.KongConsumerGroup {
		group := &configurationv1beta1.KongConsumerGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "group",
				Namespace: "default",
				UID:       k8stypes.UID("group-uid"),
			},
			Spec: configurationv1beta1.KongConsumerGroupSpec{
				Name: "group",
			},
			Status: configurationv1beta1.KongConsumerGroupStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: testCPID,
				},
			},
		}
		group.SetKonnectID("group-konnect-id")
		return group
	}
	konnectGroup := func(updatedAt int64) *sdkkonnectcomp.ConsumerGroup {
		return &sdkkonnectcomp.ConsumerGroup{
			ID:        new("group-konnect-id"),
			Name:      "group",
			UpdatedAt: new(updatedAt),
		}
	}

	testCases := []struct {
		name            string
		getResponse     *sdkkonnectops.GetConsumerGroupResponse
		expectedOutcome syncOutcome
	}{
		{
			name: "wrapped consumer group matching the upsert response is in sync",
			getResponse: &sdkkonnectops.GetConsumerGroupResponse{
				ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{ConsumerGroup: konnectGroup(2)},
			},
			expectedOutcome: syncOutcomeVerified,
		},
		{
			name: "missing wrapped consumer group is sent",
			getResponse: &sdkkonnectops.GetConsumerGroupResponse{
				ConsumerGroupInsideWrapper: &sdkkonnectcomp.ConsumerGroupInsideWrapper{},
			},
			expectedOutcome: syncOutcomeSent,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tracker, clock := newTestConfigVersionTracker()
			version := newFakeConfigVersion(t)
			sdk := mocks.NewMockConsumerGroupsSDK(t)
			group := newGroup()

			sdk.EXPECT().UpsertConsumerGroup(mock.Anything, mock.Anything).
				Return(&sdkkonnectops.UpsertConsumerGroupResponse{ConsumerGroup: konnectGroup(1)}, nil).Once()
			g := tracker.guard(version.sdk, group.GetUID(), testCPID, true)
			require.NoError(t, updateConsumerGroup(t.Context(), sdk, g, group))

			version.version = "2"
			clock.Advance(testVersionTTL)
			sdk.EXPECT().GetConsumerGroup(mock.Anything, sdkkonnectops.GetConsumerGroupRequest{
				ControlPlaneID:  testCPID,
				ConsumerGroupID: "group-konnect-id",
			}).Return(tc.getResponse, nil).Once()
			if tc.expectedOutcome == syncOutcomeSent {
				sdk.EXPECT().UpsertConsumerGroup(mock.Anything, mock.Anything).
					Return(&sdkkonnectops.UpsertConsumerGroupResponse{ConsumerGroup: konnectGroup(3)}, nil).Once()
			}

			g = tracker.guard(version.sdk, group.GetUID(), testCPID, true)
			require.NoError(t, updateConsumerGroup(t.Context(), sdk, g, group))
			assert.Equal(t, tc.expectedOutcome, g.outcome)
		})
	}
}

func TestConfigVersionTrackerConsumerGroupAssignments(t *testing.T) {
	tracker, clock := newTestConfigVersionTracker()
	version := newFakeConfigVersion(t)
	sdk := mocks.NewMockConsumersSDK(t)
	cgSDK := mocks.NewMockConsumerGroupsSDK(t)
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer",
			Namespace: "default",
			UID:       k8stypes.UID("consumer-uid"),
		},
		Username: "consumer",
		Status: configurationv1.KongConsumerStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: testCPID,
			},
		},
	}
	consumer.SetKonnectID("consumer-konnect-id")
	konnectConsumer := &sdkkonnectcomp.Consumer{ID: new("consumer-konnect-id"), Username: new("consumer")}

	expectAssignmentsReconciled := func() {
		sdk.EXPECT().ListConsumerGroupsForConsumer(mock.Anything, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
			ControlPlaneID: testCPID,
			ConsumerID:     "consumer-konnect-id",
		}).Return(&sdkkonnectops.ListConsumerGroupsForConsumerResponse{
			Object: &sdkkonnectops.ListConsumerGroupsForConsumerResponseBody{},
		}, nil).Once()
	}
	update := func() syncOutcome {
		g := tracker.guard(version.sdk, consumer.GetUID(), testCPID, true)
		require.NoError(t, updateConsumer(t.Context(), sdk, cgSDK, cl, g, consumer))
		return g.outcome
	}

	sdk.EXPECT().UpsertConsumer(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpsertConsumerResponse{Consumer: konnectConsumer}, nil).Once()
	expectAssignmentsReconciled()
	require.Equal(t, syncOutcomeSent, update())

	require.Equal(t, syncOutcomeSkipped, update(), "skipped update must not reconcile consumer group assignments")

	version.version = "2"
	clock.Advance(testVersionTTL)
	sdk.EXPECT().GetConsumer(mock.Anything, mock.Anything, mock.Anything).
		Return(&sdkkonnectops.GetConsumerResponse{Consumer: konnectConsumer}, nil).Once()
	expectAssignmentsReconciled()
	require.Equal(t, syncOutcomeVerified, update(), "verified update must reconcile consumer group assignments")
}

func TestRepresentationHash(t *testing.T) {
	a, err := representationHash(&sdkkonnectcomp.Service{
		ID: new("id"), Host: "example.com", CreatedAt: new(int64(1)), UpdatedAt: new(int64(1)),
	})
	require.NoError(t, err)
	b, err := representationHash(&sdkkonnectcomp.Service{
		ID: new("id"), Host: "example.com", CreatedAt: new(int64(2)), UpdatedAt: new(int64(2)),
	})
	require.NoError(t, err)
	assert.Equal(t, a, b, "timestamps must be ignored")

	c, err := representationHash(&sdkkonnectcomp.Service{ID: new("id"), Host: "other.example.com"})
	require.NoError(t, err)
	assert.NotEqual(t, a, c)

	d, err := representationHash(map[string]any{"b": 1, "a": map[string]any{"y": 1, "x": 2}})
	require.NoError(t, err)
	e, err := representationHash(map[string]any{"a": map[string]any{"x": 2, "y": 1}, "b": 1})
	require.NoError(t, err)
	assert.Equal(t, d, e, "key order must be ignored")
}

func TestUpdateSkipsPeriodicUpdateInSyncWithKonnect(t *testing.T) {
	tracker, _ := newTestConfigVersionTracker()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	services := newFakeKonnectServices(t)
	sdk.ServicesSDK = services.sdk
	version := newFakeConfigVersion(t)
	sdk.DPNodesSDK = version.sdk
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
	svc := newTestKongService()
	svc.Generation = 1
	syncPeriod := time.Minute

	_, err := Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, svc, tracker)
	require.NoError(t, err)
	_, upserts := services.calls()
	require.Equal(t, 1, upserts)

	lastUpdate := metav1.NewTime(time.Now().Add(-2 * syncPeriod).Truncate(time.Second))
	svc.Status.Conditions[0].LastTransitionTime = lastUpdate

	res, err := Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, svc, tracker)
	require.NoError(t, err)
	assert.Equal(t, syncPeriod, res.RequeueAfter)
	gets, upserts := services.calls()
	assert.Equal(t, 0, gets)
	assert.Equal(t, 0, upserts)
	require.Len(t, svc.Status.Conditions, 1)
	assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedConditionType, svc.Status.Conditions[0].Type)
	assert.Equal(t, lastUpdate, svc.Status.Conditions[0].LastTransitionTime, "skipped update must not touch the status")

	svc.Generation = 2
	_, err = Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, svc, tracker)
	require.NoError(t, err)
	_, upserts = services.calls()
	assert.Equal(t, 1, upserts, "generation change must be sent")
}
