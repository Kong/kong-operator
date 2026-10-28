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
	"net/http"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

// The state of a Syncer, in memory. None of it survives a restart of the
// operator, and none needs to: losing it costs at most one more call to
// gw-admin.

// keyedLocks locks GWAdmins one by one: the holders of the lock of a GWAdmin
// run one at a time, those of different GWAdmins concurrently. Those that
// don't get the lock of a GWAdmin wait in line for it: each unlock wakes the
// first of them. The zero value is ready to use.
type keyedLocks struct {
	mu sync.Mutex
	// locked holds the GWAdmins locked.
	locked map[types.NamespacedName]struct{}
	// lines holds, by GWAdmin, those waiting for its lock, in the order they
	// tried.
	lines map[types.NamespacedName][]types.NamespacedName
}

// tryLock locks the key, unless it is locked already: then waiter waits in
// line for it. It returns the function unlocking it, which returns the first
// in line, if any, to wake.
func (k *keyedLocks) tryLock(key, waiter types.NamespacedName) (unlock func() (wake types.NamespacedName, ok bool), locked bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.locked == nil {
		k.locked = map[types.NamespacedName]struct{}{}
		k.lines = map[types.NamespacedName][]types.NamespacedName{}
	}
	if _, held := k.locked[key]; held {
		if !slices.Contains(k.lines[key], waiter) {
			k.lines[key] = append(k.lines[key], waiter)
		}
		return nil, false
	}
	k.locked[key] = struct{}{}
	k.leaveLine(key, waiter)
	return func() (types.NamespacedName, bool) {
		k.mu.Lock()
		defer k.mu.Unlock()
		delete(k.locked, key)
		line := k.lines[key]
		if len(line) == 0 {
			return types.NamespacedName{}, false
		}
		first := line[0]
		k.leaveLine(key, first)
		return first, true
	}, true
}

// leaveLine takes the waiter out of the line of the key. k.mu must be held.
func (k *keyedLocks) leaveLine(key, waiter types.NamespacedName) {
	line := slices.DeleteFunc(k.lines[key], func(w types.NamespacedName) bool { return w == waiter })
	if len(line) == 0 {
		delete(k.lines, key)
		return
	}
	k.lines[key] = line
}

// epochs counts, by GWAdmin, how many times what is kept of it was forgotten,
// e.g. once it is deleted: what a call resolved before can't be kept anymore,
// as it would never be forgotten. The zero value is ready to use.
type epochs struct {
	mu sync.Mutex
	// byGWAdmin holds the epoch of each GWAdmin.
	byGWAdmin map[types.NamespacedName]uint64
}

// current returns the epoch of the GWAdmin.
func (e *epochs) current(key types.NamespacedName) uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.byGWAdmin[key]
}

// keep runs keep, unless the GWAdmin was forgotten since its epoch was the
// one given, and reports whether it ran. Forgetting it waits for it.
func (e *epochs) keep(key types.NamespacedName, epoch uint64, keep func()) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.byGWAdmin[key] != epoch {
		return false
	}
	keep()
	return true
}

// forget starts a new epoch of the GWAdmin, and runs forget.
func (e *epochs) forget(key types.NamespacedName, forget func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.byGWAdmin == nil {
		e.byGWAdmin = map[types.NamespacedName]uint64{}
	}
	e.byGWAdmin[key]++
	forget()
}

// transportCache keeps the HTTP transport to the gw-admin pods of each
// GWAdmin, so that the connections to them are reused across calls. A
// transport is replaced when the TLS material of the GWAdmin changes, e.g.
// once a certificate is renewed. The zero value is ready to use.
type transportCache struct {
	mu        sync.Mutex
	byGWAdmin map[types.NamespacedName]cachedTransport
}

type cachedTransport struct {
	// fingerprint tells the TLS material the transport was built with.
	fingerprint string
	transport   *http.Transport
}

// get returns the transport of the GWAdmin for the TLS material whose
// fingerprint is given, built by newTransport when there is none yet.
func (c *transportCache) get(key types.NamespacedName, fingerprint string, newTransport func() *http.Transport) *http.Transport {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.byGWAdmin[key]; ok {
		if cached.fingerprint == fingerprint {
			return cached.transport
		}
		// A request still using it completes: only its idle connections
		// are closed.
		cached.transport.CloseIdleConnections()
	}
	if c.byGWAdmin == nil {
		c.byGWAdmin = map[types.NamespacedName]cachedTransport{}
	}
	t := newTransport()
	c.byGWAdmin[key] = cachedTransport{fingerprint: fingerprint, transport: t}
	return t
}

// forget drops the transport of a GWAdmin that is gone.
func (c *transportCache) forget(key types.NamespacedName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.byGWAdmin[key]; ok {
		cached.transport.CloseIdleConnections()
		delete(c.byGWAdmin, key)
	}
}

// podRecords holds what was recorded of each gw-admin pod, by GWAdmin, then
// by pod (Pod.Key), then by item. The zero value is ready to use.
type podRecords struct {
	mu        sync.Mutex
	byGWAdmin map[types.NamespacedName]map[string]map[string]string
}

// get returns the value of the item on the pod, or "".
func (r *podRecords) get(gwAdmin types.NamespacedName, pod, item string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byGWAdmin[gwAdmin][pod][item]
}

// set sets the value of the item on the pod.
func (r *podRecords) set(gwAdmin types.NamespacedName, pod, item, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byGWAdmin == nil {
		r.byGWAdmin = map[types.NamespacedName]map[string]map[string]string{}
	}
	pods := r.byGWAdmin[gwAdmin]
	if pods == nil {
		pods = map[string]map[string]string{}
		r.byGWAdmin[gwAdmin] = pods
	}
	items := pods[pod]
	if items == nil {
		items = map[string]string{}
		pods[pod] = items
	}
	items[item] = value
}

// unset forgets the item on the pod.
func (r *podRecords) unset(gwAdmin types.NamespacedName, pod, item string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byGWAdmin[gwAdmin][pod], item)
}

// keepPods forgets the pods of the GWAdmin but those given.
func (r *podRecords) keepPods(gwAdmin types.NamespacedName, pods []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for pod := range r.byGWAdmin[gwAdmin] {
		if !slices.Contains(pods, pod) {
			delete(r.byGWAdmin[gwAdmin], pod)
		}
	}
}

// forgetGWAdmin forgets the pods of the GWAdmin.
func (r *podRecords) forgetGWAdmin(gwAdmin types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byGWAdmin, gwAdmin)
}
