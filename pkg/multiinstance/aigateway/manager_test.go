package aigateway

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
)

func newTestInstance() *Instance {
	return NewInstance(manager.NewRandomID(), logr.Discard(), Config{}, Env{})
}

func TestManagerWaitForInstanceStop(t *testing.T) {
	t.Run("unknown instance returns at once", func(t *testing.T) {
		m := NewManager(logr.Discard())
		require.NoError(t, m.WaitForInstanceStop(t.Context(), manager.NewRandomID()))
	})

	t.Run("instance stopped before it ran", func(t *testing.T) {
		// The registry is not started, so the instance never runs.
		m := NewManager(logr.Discard())
		in := newTestInstance()
		require.NoError(t, m.ScheduleInstance(in))
		require.NoError(t, m.StopInstance(in.ID()))

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		require.NoError(t, m.WaitForInstanceStop(ctx, in.ID()))
		require.NoError(t, in.Run(t.Context()), "Run after the stop must return at once")
		require.Eventually(t, func() bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			_, ok := m.scheduled[in.ID()]
			return !ok
		}, 5*time.Second, 10*time.Millisecond, "a stopped instance must not stay tracked")
	})

	t.Run("running instance blocks until ctx is done", func(t *testing.T) {
		m := NewManager(logr.Discard())
		in := newTestInstance()
		require.NoError(t, m.ScheduleInstance(in))
		// Pretend Run started: the stop alone must not close done.
		in.running = true
		require.NoError(t, m.StopInstance(in.ID()))

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, m.WaitForInstanceStop(ctx, in.ID()), context.DeadlineExceeded)

		in.closeDone()
		require.NoError(t, m.WaitForInstanceStop(t.Context(), in.ID()))
	})
}
