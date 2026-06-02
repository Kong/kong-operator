package mcpserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newTestSignalManager creates a SignalManager with a cancelled parent context so
// any goroutines launched by registerControlPlane exit immediately.
func newTestSignalManager(t *testing.T) *SignalManager {
	t.Helper()
	s := scheme.Get()
	reconcileEventCh := make(chan event.GenericEvent, TriggerChannelBufSize)
	sm := NewSignalManager(logging.DevelopmentMode, nil, s, reconcileEventCh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so derived contexts are immediately done

	sm.mu.Lock()
	sm.parent = ctx
	sm.mu.Unlock()

	return sm
}

func TestSignalManager_NotifyMCPServerDeleted(t *testing.T) {
	t.Run("unregistered CP is a no-op", func(t *testing.T) {
		sm := newTestSignalManager(t)
		// Must not panic or block.
		sm.NotifyMCPServerDeleted("default", "nonexistent")
	})

	t.Run("registered CP receives signal", func(t *testing.T) {
		sm := newTestSignalManager(t)
		resetCh := make(chan struct{}, 1)

		sm.mu.Lock()
		sm.resetChs["default/my-cp"] = resetCh
		sm.mu.Unlock()

		sm.NotifyMCPServerDeleted("default", "my-cp")

		select {
		case <-resetCh:
			// signal received as expected
		default:
			t.Fatal("expected reset signal but channel was empty")
		}
	})

	t.Run("full buffer drops notification without blocking", func(t *testing.T) {
		sm := newTestSignalManager(t)
		resetCh := make(chan struct{}, 1)

		// Fill the buffer so the channel is already full.
		resetCh <- struct{}{}

		sm.mu.Lock()
		sm.resetChs["default/my-cp"] = resetCh
		sm.mu.Unlock()

		// Must not block even though the channel is full.
		sm.NotifyMCPServerDeleted("default", "my-cp")

		// Channel should still have exactly one item (the original one).
		assert.Len(t, resetCh, 1)
	})
}

func TestSignalManager_DeregisterControlPlane(t *testing.T) {
	makeCP := func(name, namespace string) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "konnect-id"},
			},
		}
	}

	t.Run("deregistering unregistered CP is a no-op", func(t *testing.T) {
		sm := newTestSignalManager(t)
		cp := makeCP("my-cp", "default")

		// Must not panic or modify maps.
		sm.deregisterControlPlane(cp)

		sm.mu.Lock()
		defer sm.mu.Unlock()
		assert.Empty(t, sm.routines)
		assert.Empty(t, sm.resetChs)
	})

	t.Run("deregistering registered CP cancels context and cleans up maps", func(t *testing.T) {
		sm := newTestSignalManager(t)
		cp := makeCP("my-cp", "default")
		key := "default/my-cp"

		// Manually register the CP in the maps with a real cancellable context.
		innerCtx, innerCancel := context.WithCancel(context.Background())
		t.Cleanup(innerCancel)
		sm.mu.Lock()
		sm.routines[key] = innerCancel
		sm.resetChs[key] = make(chan struct{}, 1)
		sm.mu.Unlock()

		sm.deregisterControlPlane(cp)

		// Context must be cancelled.
		require.Error(t, innerCtx.Err(), "expected context to be cancelled after deregistration")

		// Maps must be cleaned up.
		sm.mu.Lock()
		defer sm.mu.Unlock()
		_, hasRoutine := sm.routines[key]
		_, hasResetCh := sm.resetChs[key]
		assert.False(t, hasRoutine, "routine entry should be removed")
		assert.False(t, hasResetCh, "resetCh entry should be removed")
	})
}

func TestSignalManager_RegisterControlPlane(t *testing.T) {
	makeCP := func(name, namespace string) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "konnect-id"},
			},
		}
	}

	t.Run("first registration populates maps", func(t *testing.T) {
		sm := newTestSignalManager(t)
		cp := makeCP("my-cp", "default")

		// Pass nil as konnectClient — goroutines will exit immediately due to
		// the cancelled parent context before making any SDK calls.
		sm.registerControlPlane(nil, cp)

		sm.mu.Lock()
		defer sm.mu.Unlock()
		key := "default/my-cp"
		assert.Contains(t, sm.routines, key, "routine entry should be added")
		assert.Contains(t, sm.resetChs, key, "resetCh entry should be added")
	})

	t.Run("second registration for same CP is a no-op", func(t *testing.T) {
		sm := newTestSignalManager(t)
		cp := makeCP("my-cp", "default")
		key := "default/my-cp"

		// Pre-populate with a sentinel cancel func so we can detect if it gets replaced.
		sentinelCalled := false
		sm.mu.Lock()
		sm.routines[key] = func() { sentinelCalled = true }
		sm.resetChs[key] = make(chan struct{}, 1)
		sm.mu.Unlock()

		sm.registerControlPlane(nil, cp)

		// Sentinel cancel func must not have been replaced or called.
		sm.mu.Lock()
		cancelFn := sm.routines[key]
		sm.mu.Unlock()
		cancelFn()
		assert.True(t, sentinelCalled, "original cancel func should still be in place")
	})
}
