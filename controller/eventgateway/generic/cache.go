/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package generic

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/controller/eventgateway/admin"
)

// defaultCacheBuffer is the default buffer size for the cache's intake channel.
const defaultCacheBuffer = 256

// snapshotInterval is how often, after an initial delay of the same length,
// the cache rebuilds its per-gateway EventGatewaySnapshot from the current
// contents.
const snapshotInterval = 2 * time.Second

// snapshotWatchInterval is how often the snapshot watcher polls the published
// snapshots map to detect changes.
const snapshotWatchInterval = 1 * time.Second

type cacheOp int

const (
	cacheOpUpsert cacheOp = iota
	cacheOpDelete
)

// cacheKey identifies an entry in the cache. The Go type is part of the key
// so different kinds (e.g. Listener vs BackendCluster) sharing a single cache
// can have entries with the same namespace/name without colliding.
type cacheKey struct {
	typ reflect.Type
	nsn client.ObjectKey
}

func (k cacheKey) String() string {
	return fmt.Sprintf("%s/%s", k.typ.String(), k.nsn)
}

func keyOf(obj client.Object) cacheKey {
	return cacheKey{
		typ: reflect.TypeOf(obj),
		nsn: client.ObjectKeyFromObject(obj),
	}
}

type cacheEvent struct {
	op        cacheOp
	gatewayID string
	key       cacheKey
	obj       client.Object
}

// ObjectCache asynchronously stores objects pushed via Push and removes those
// popped via Pop. Items are bucketed by parent EventGateway ID. A background
// routine drains the intake channel and applies upsert/delete events to an
// in-memory two-level map. Pushes and pops share the same channel so they're
// applied in submission order. The cache is safe to share across multiple
// Reconciler instances of different kinds. Implements controller-runtime's
// manager.Runnable.
type ObjectCache struct {
	ch    chan cacheEvent
	mu    sync.RWMutex
	items map[string]map[cacheKey]client.Object

	snapshots atomic.Pointer[map[string]EventGatewaySnapshot]
	admin     *admin.Client

	addOnce sync.Once
	addErr  error
}

// NewObjectCache returns an empty ObjectCache. If adminClient is non-nil, the
// cache's snapshot watcher will push every snapshot change to the admin API.
func NewObjectCache(adminClient *admin.Client) *ObjectCache {
	return &ObjectCache{
		ch:    make(chan cacheEvent, defaultCacheBuffer),
		items: make(map[string]map[cacheKey]client.Object),
		admin: adminClient,
	}
}

// AddTo registers the cache as a Runnable on mgr exactly once, even when
// invoked from multiple SetupWithManager calls that share the same cache.
func (c *ObjectCache) AddTo(mgr ctrl.Manager) error {
	c.addOnce.Do(func() {
		c.addErr = mgr.Add(c)
	})
	return c.addErr
}

// Start drains the intake channel into the internal cache until ctx is done,
// and periodically rebuilds the per-gateway EventGatewaySnapshot from the
// cache content. Satisfies sigs.k8s.io/controller-runtime/pkg/manager.Runnable.
func (c *ObjectCache) Start(ctx context.Context) error {
	go c.refreshSnapshots(ctx)
	go c.watchSnapshots(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-c.ch:
			c.mu.Lock()
			switch ev.op {
			case cacheOpUpsert:
				bucket, ok := c.items[ev.gatewayID]
				if !ok {
					bucket = make(map[cacheKey]client.Object)
					c.items[ev.gatewayID] = bucket
				}
				bucket[ev.key] = ev.obj
			case cacheOpDelete:
				if ev.gatewayID != "" {
					if bucket, ok := c.items[ev.gatewayID]; ok {
						delete(bucket, ev.key)
						if len(bucket) == 0 {
							delete(c.items, ev.gatewayID)
						}
					}
					break
				}
				// Unknown gateway ID at delete time (e.g. tombstone): scan all
				// buckets and remove the matching key.
				for gid, bucket := range c.items {
					if _, ok := bucket[ev.key]; ok {
						delete(bucket, ev.key)
						if len(bucket) == 0 {
							delete(c.items, gid)
						}
					}
				}
			}
			c.mu.Unlock()
		}
	}
}

// refreshSnapshots rebuilds the per-gateway EventGatewaySnapshot map at every
// snapshotInterval tick. The first tick fires after one interval, providing
// the requested initial delay. The latest snapshots map is published
// atomically and read via Snapshot().
func (c *ObjectCache) refreshSnapshots(ctx context.Context) {
	ticker := time.NewTicker(snapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			snaps := make(map[string]EventGatewaySnapshot, len(c.items))
			for gid, bucket := range c.items {
				snaps[gid] = buildEventGatewaySnapshot(bucket)
			}
			c.mu.RUnlock()
			c.snapshots.Store(&snaps)
		}
	}
}

// Snapshot returns the most recently built EventGatewaySnapshot for the given
// gateway ID, or nil if none has been produced for it yet.
func (c *ObjectCache) Snapshot(gatewayID string) *EventGatewaySnapshot {
	snaps := c.snapshots.Load()
	if snaps == nil {
		return nil
	}
	snap, ok := (*snaps)[gatewayID]
	if !ok {
		return nil
	}
	return &snap
}

// watchSnapshots polls the published per-gateway snapshots and pushes each
// changed snapshot to the admin API. It is fully decoupled from
// refreshSnapshots: it only reads c.snapshots via the atomic pointer.
func (c *ObjectCache) watchSnapshots(ctx context.Context) {
	logger := ctrllog.FromContext(ctx).WithName("eventgateway-snapshot-watcher")

	if c.admin == nil {
		logger.Info("admin client not configured; skipping snapshot push")
		return
	}

	ticker := time.NewTicker(snapshotWatchInterval)
	defer ticker.Stop()

	last := map[string]EventGatewaySnapshot{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := c.snapshots.Load()
			if cur == nil {
				continue
			}
			for gid, snap := range *cur {
				if prev, ok := last[gid]; ok && reflect.DeepEqual(prev, snap) {
					continue
				}
				if err := c.admin.PushSnapshot(ctx, gid, snap); err != nil {
					logger.Error(err, "failed to push EventGatewaySnapshot", "gatewayID", gid)
					continue
				}
				last[gid] = snap
			}
		}
	}
}

// Push enqueues obj to be stored in the cache under the given gateway ID.
func (c *ObjectCache) Push(gatewayID string, obj client.Object) {
	c.ch <- cacheEvent{
		op:        cacheOpUpsert,
		gatewayID: gatewayID,
		key:       keyOf(obj),
		obj:       obj,
	}
}

// Pop enqueues a deletion of the entry identified by (sample's Go type, key).
// sample is used only for type identification — its name/namespace are
// ignored. The entry is removed from whichever gateway bucket contains it.
func (c *ObjectCache) Pop(sample client.Object, key client.ObjectKey) {
	c.ch <- cacheEvent{
		op: cacheOpDelete,
		key: cacheKey{
			typ: reflect.TypeOf(sample),
			nsn: key,
		},
	}
}

// Get returns the cached object for (gatewayID, sample's Go type, key), or
// nil and false.
func (c *ObjectCache) Get(gatewayID string, sample client.Object, key client.ObjectKey) (client.Object, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	bucket, ok := c.items[gatewayID]
	if !ok {
		return nil, false
	}
	obj, ok := bucket[cacheKey{typ: reflect.TypeOf(sample), nsn: key}]
	return obj, ok
}
