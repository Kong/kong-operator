package changenotifier

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func testObject(uid types.UID) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Name:      "test",
		Namespace: "test",
		UID:       uid,
	}
}

func TestNotifyChange_SendsChange(t *testing.T) {
	n := New()
	defer n.Close()

	parent := &types.NamespacedName{Name: "gw", Namespace: "ns"}
	n.NotifyChange(t.Context(), parent, testObject("uid-1"))

	select {
	case change := <-n.NotifyChannel():
		require.Equal(t, types.UID("uid-1"), change.ID)
		require.Equal(t, parent, change.ParentNN)
		require.NotNil(t, change.Object)
	default:
		t.Fatal("expected a change notification")
	}
}

func TestNotifyChange_DropsWhenBufferFull(t *testing.T) {
	n := New()
	defer n.Close()

	for range cap(n.ch) {
		n.NotifyChange(t.Context(), nil, testObject("uid"))
	}
	// Buffer is full: this must not block and must be dropped.
	n.NotifyChange(t.Context(), nil, testObject("uid-overflow"))

	require.Len(t, n.ch, cap(n.ch))
}

func TestNotifyChange_ContextCancelled(t *testing.T) {
	n := New()
	defer n.Close()

	// Fill the buffer so the send case is not ready; with a cancelled ctx the
	// call must return via ctx.Done instead of blocking on the send.
	for range cap(n.ch) {
		n.NotifyChange(t.Context(), nil, testObject("uid"))
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.NotifyChange(ctx, nil, testObject("uid-blocked"))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NotifyChange blocked despite cancelled context")
	}
}

func TestNotifyChange_AfterCloseDoesNotPanic(t *testing.T) {
	n := New()
	n.Close()

	// Must not block or panic: the closedCh case makes the select return immediately.
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.NotifyChange(t.Context(), nil, testObject("uid"))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NotifyChange blocked after Close")
	}
}

func TestNotifyChannel_ClosedAfterClose(t *testing.T) {
	n := New()
	ch := n.NotifyChannel()
	n.Close()

	// A channel taken before Close is closed: receiving yields the zero value immediately.
	select {
	case <-ch:
	default:
		t.Fatal("expected the pre-Close channel to be closed")
	}
}

func TestClose_Idempotent(t *testing.T) {
	n := New()
	n.Close()
	n.Close()
	n.Close()
}

func TestConcurrentNotifyAndClose(t *testing.T) {
	n := New()
	parent := &types.NamespacedName{Name: "gw", Namespace: "ns"}

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for range 32 {
				n.NotifyChange(t.Context(), parent, testObject(types.UID(string(rune(i)))))
			}
		})
	}
	time.Sleep(10 * time.Millisecond)
	n.Close()
	wg.Wait()
}
