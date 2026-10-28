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
	"cmp"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// The defaults of a Backoff.
const (
	defaultBackoffBase = 2 * time.Second
	defaultBackoffMax  = 2 * time.Minute
)

// Backoff delays the push of an object that failed, whatever triggers it:
// e.g. every change to the object, its status too, which a failed push
// writes. A push whose inputs changed, e.g. once a pod is replaced or a
// certificate renewed, isn't delayed. The zero value is ready to use.
type Backoff struct {
	// Base is the wait before a push that failed is tried again, after its
	// first failure. It doubles with every failure. It is 2s when zero.
	Base time.Duration
	// Max bounds the wait before a push that failed is tried again. It is
	// 2m when zero.
	Max time.Duration

	mu       sync.Mutex
	byObject map[types.UID]backoffEntry
}

type backoffEntry struct {
	// inputs tells what the failed push was.
	inputs string
	// err is the error of the last push that failed.
	err       error
	failures  int
	notBefore time.Time
}

// Wait returns how long the push of the object with these inputs must still
// wait, or 0, and the error of the last push that failed: e.g. to report it
// again while waiting.
func (b *Backoff) Wait(object types.UID, inputs string, now time.Time) (time.Duration, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.byObject[object]
	if !ok || e.inputs != inputs || !now.Before(e.notBefore) {
		return 0, nil
	}
	return e.notBefore.Sub(now), e.err
}

// Failed records that the push of the object with these inputs failed with
// the error, and returns how long the next one waits.
func (b *Backoff) Failed(object types.UID, inputs string, err error, now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byObject == nil {
		b.byObject = map[types.UID]backoffEntry{}
	}
	e := b.byObject[object]
	if e.inputs != inputs {
		e = backoffEntry{inputs: inputs}
	}
	base, maxDelay := cmp.Or(b.Base, defaultBackoffBase), cmp.Or(b.Max, defaultBackoffMax)
	delay := maxDelay
	if e.failures < 16 {
		delay = min(base<<e.failures, maxDelay)
	}
	e.failures++
	e.err = err
	e.notBefore = now.Add(delay)
	b.byObject[object] = e
	return delay
}

// Forget forgets the failures of the object, e.g. once it is pushed.
func (b *Backoff) Forget(object types.UID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byObject, object)
}
