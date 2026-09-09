package instances_test

import (
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

const (
	waitTime = time.Second
	tickTime = time.Millisecond * 10
)

func TestRegistry_Scheduling(t *testing.T) {
	onCleanupVerifyThereAreNoLeakedGoroutines(t)

	// Such context will be canceled just before the test ends (Cleanups are run)
	// so we can ensure all goroutines are cleaned up.
	ctx := t.Context()

	registry := instances.NewRegistry(testr.New(t))

	mockInstance1 := newMockInstance(manager.NewRandomID())
	mockInstance2 := newMockInstance(manager.NewRandomID())

	t.Run("can schedule instances before starting the registry", func(t *testing.T) {
		err := registry.ScheduleInstance(mockInstance1)
		require.NoError(t, err)

		err = registry.ScheduleInstance(mockInstance2)
		require.NoError(t, err)

		require.False(t, mockInstance1.wasStarted.Load(), "instance should not have been started yet as the registry is not running")
	})

	t.Run("scheduling an instance with the same ID should fail", func(t *testing.T) {
		err := registry.ScheduleInstance(mockInstance1)
		require.ErrorIs(t, err, instances.NewInstanceWithIDAlreadyScheduledError(mockInstance1.ID()))
	})

	registryRunning := make(chan struct{})
	t.Run("can run the registry", func(t *testing.T) {
		go func() {
			close(registryRunning)
			assert.NoError(t, registry.Start(ctx))
		}()
	})

	t.Run("can schedule instances after starting the registry", func(t *testing.T) {
		<-registryRunning // Wait for the registry to start.

		mockInstance3 := newMockInstance(manager.NewRandomID())
		err := registry.ScheduleInstance(mockInstance3)
		require.NoError(t, err)

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			assert.True(t, mockInstance1.wasStarted.Load())
		}, waitTime, tickTime)
	})

	t.Run("can stop an instance", func(t *testing.T) {
		err := registry.StopInstance(mockInstance1.ID())
		require.NoError(t, err)

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			assert.True(t, mockInstance1.wasContextCanceled.Load())
		}, waitTime, tickTime)
	})

	t.Run("can inspect instance readiness", func(t *testing.T) {
		err := registry.IsInstanceReady(mockInstance2.ID())
		require.NoError(t, err)

		// Deletion from the instance map happens asynchronously in the registry
		// so use require.EventuallyWithT to wait for the error to be returned.
		// Otherwise it may happen that the error is not returned immediately
		// because the information hasn't been sent on StopChannel yet.
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := registry.IsInstanceReady(mockInstance1.ID())
			require.ErrorIs(t, err, instances.NewInstanceNotFoundError(mockInstance1.ID()))
		}, waitTime, tickTime)
	})
}

func TestRegistry_WithDiagnosticsExposer(t *testing.T) {
	onCleanupVerifyThereAreNoLeakedGoroutines(t)

	ctx := t.Context()

	t.Log("Configuring a registry with a diagnostics exposer")
	diagnosticsExposer := newMockDiagnosticsExposer()
	registry := instances.NewRegistry(testr.New(t), instances.WithDiagnosticsExposer(diagnosticsExposer))

	go func() {
		assert.NoError(t, registry.Start(ctx))
	}()

	instanceID1 := manager.NewRandomID()
	instanceID2 := manager.NewRandomID()

	t.Log("Scheduling first instance")
	err := registry.ScheduleInstance(newMockInstance(instanceID1))
	require.NoError(t, err)

	t.Log("Expecting the diagnostics exposer to have the first instance registered")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Contains(t, diagnosticsExposer.RegisteredInstances(), instanceID1)
		assert.NotContains(t, diagnosticsExposer.RegisteredInstances(), instanceID2)
	}, waitTime, tickTime)

	t.Log("Scheduling second instance")
	err = registry.ScheduleInstance(newMockInstance(instanceID2))
	require.NoError(t, err)

	t.Log("Expecting the diagnostics exposer to have both instances registered")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.ElementsMatch(t, diagnosticsExposer.RegisteredInstances(), []manager.ID{instanceID1, instanceID2})
	}, waitTime, tickTime)

	t.Log("Stopping first instance")
	err = registry.StopInstance(instanceID1)
	require.NoError(t, err)

	t.Log("Expecting the diagnostics exposer to have only the second instance registered")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Contains(t, diagnosticsExposer.RegisteredInstances(), instanceID2)
		assert.NotContains(t, diagnosticsExposer.RegisteredInstances(), instanceID1)
	}, waitTime, tickTime)

	t.Log("Stopping second instance")
	err = registry.StopInstance(instanceID2)
	require.NoError(t, err)

	t.Log("Expecting the diagnostics exposer to have no instances registered")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.Empty(t, diagnosticsExposer.RegisteredInstances())
	}, waitTime, tickTime)
}

// onCleanupVerifyThereAreNoLeakedGoroutines is a helper function that sets up a cleanup function to verify there are no
// leaked goroutines at the end of the test.
func onCleanupVerifyThereAreNoLeakedGoroutines(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { goleak.VerifyNone(t) })
}
