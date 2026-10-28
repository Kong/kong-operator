/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gwadminsync

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestTryLock(t *testing.T) {
	var s Syncer
	a := types.NamespacedName{Namespace: "ns", Name: "a"}
	b := types.NamespacedName{Namespace: "ns", Name: "b"}
	gateway := func(name string) types.NamespacedName { return types.NamespacedName{Namespace: "ns", Name: name} }
	woken := func() []string {
		var names []string
		for {
			select {
			case e := <-s.wakeupChannel():
				names = append(names, e.Object.GetName())
			default:
				return names
			}
		}
	}

	unlockA, ok := s.TryLock(a, gateway("first"))
	require.True(t, ok)
	_, ok = s.TryLock(a, gateway("second"))
	assert.False(t, ok, "the lock of a GWAdmin is held once at a time")
	_, ok = s.TryLock(a, gateway("third"))
	assert.False(t, ok)
	_, ok = s.TryLock(a, gateway("second"))
	assert.False(t, ok, "a waiter is in line once")
	unlockB, ok := s.TryLock(b, gateway("other"))
	assert.True(t, ok, "the lock of another GWAdmin is not")
	assert.Empty(t, woken())

	unlockA()
	assert.Equal(t, []string{"second"}, woken(), "an unlock wakes the first in line only")
	unlockA, ok = s.TryLock(a, gateway("second"))
	require.True(t, ok, "the lock of a GWAdmin is acquired once released")
	unlockA()
	assert.Equal(t, []string{"third"}, woken(), "then the next one")
	unlockA, ok = s.TryLock(a, gateway("third"))
	require.True(t, ok)
	unlockA()
	unlockB()
	assert.Empty(t, woken(), "nobody waits anymore")

	s.locks.mu.Lock()
	defer s.locks.mu.Unlock()
	assert.Empty(t, s.locks.locked, "a lock nobody holds is dropped")
	assert.Empty(t, s.locks.lines)
}

func TestTryLockGWAdmin(t *testing.T) {
	var s Syncer
	// The gateway shares the name of its GWAdmin: they still wait apart.
	a := types.NamespacedName{Namespace: "ns", Name: "a"}
	gateway := func(name string) types.NamespacedName { return types.NamespacedName{Namespace: "ns", Name: name} }
	woken := func(wakeups chan event.GenericEvent) []string {
		var names []string
		for {
			select {
			case e := <-wakeups:
				names = append(names, e.Object.GetName())
			default:
				return names
			}
		}
	}

	unlock, ok := s.TryLock(a, gateway("holder"))
	require.True(t, ok)
	_, ok = s.TryLockGWAdmin(a)
	assert.False(t, ok, "the lock of a GWAdmin is held once at a time")
	_, ok = s.TryLock(a, a)
	require.False(t, ok)
	_, ok = s.TryLock(a, gateway("last"))
	require.False(t, ok)
	unlock()
	assert.Equal(t, []string{"a"}, woken(s.gwAdminWakeupChannel()), "the GWAdmin was first in line")
	assert.Equal(t, []string{"a"}, woken(s.wakeupChannel()),
		"the gateway after it too: the reconcile of the GWAdmin may not take the lock, and wake nobody")

	// The gateway takes it, and wakes the next one only.
	unlock, ok = s.TryLock(a, a)
	require.True(t, ok)
	unlock()
	assert.Equal(t, []string{"last"}, woken(s.wakeupChannel()))
	assert.Empty(t, woken(s.gwAdminWakeupChannel()))

	s.locks.mu.Lock()
	defer s.locks.mu.Unlock()
	assert.Empty(t, s.locks.locked)
	assert.Empty(t, s.locks.lines)
}

func TestTryLockWakesWithoutBlocking(t *testing.T) {
	var s Syncer
	a := types.NamespacedName{Namespace: "ns", Name: "a"}
	for i := range wakeupBuffer + 1 {
		unlock, ok := s.TryLock(a, types.NamespacedName{Namespace: "ns", Name: "holder"})
		require.True(t, ok)
		_, ok = s.TryLock(a, types.NamespacedName{Namespace: "ns", Name: fmt.Sprintf("waiter-%d", i)})
		require.False(t, ok)
		unlock()
	}
	assert.Len(t, s.wakeupChannel(), wakeupBuffer, "past its buffer, a wakeup is dropped: the waiter is requeued anyway")
}

func TestEpochs(t *testing.T) {
	var e epochs
	key := types.NamespacedName{Namespace: "ns", Name: "gw-admin"}
	epoch := e.current(key)
	kept := 0
	assert.True(t, e.keep(key, epoch, func() { kept++ }))
	e.forget(key, func() {})
	assert.False(t, e.keep(key, epoch, func() { kept++ }), "what was resolved before it was forgotten isn't kept")
	assert.Equal(t, 1, kept)
	assert.True(t, e.keep(key, e.current(key), func() { kept++ }))
}

func TestTransportCache(t *testing.T) {
	var cache transportCache
	key := types.NamespacedName{Namespace: "ns", Name: "gw-admin"}
	built := 0
	newTransport := func() *http.Transport {
		built++
		return &http.Transport{}
	}

	first := cache.get(key, "v1", newTransport)
	assert.Same(t, first, cache.get(key, "v1", newTransport), "reused while the TLS material is the same")
	renewed := cache.get(key, "v2", newTransport)
	assert.NotSame(t, first, renewed, "replaced once it changes")
	assert.Equal(t, 2, built)

	cache.forget(key)
	assert.NotSame(t, renewed, cache.get(key, "v2", newTransport), "a GWAdmin gone is forgotten")
	assert.Equal(t, 3, built)
}
