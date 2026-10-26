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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff(t *testing.T) {
	var b Backoff
	now := time.Now()
	failed := errors.New("pod down")
	wait := func(inputs string, at time.Time) time.Duration {
		d, _ := b.Wait("object", inputs, at)
		return d
	}
	assert.Zero(t, wait("inputs", now), "never failed")

	assert.Equal(t, defaultBackoffBase, b.Failed("object", "inputs", failed, now))
	d, err := b.Wait("object", "inputs", now)
	assert.Equal(t, defaultBackoffBase, d)
	assert.Equal(t, failed, err, "the error of the last push that failed")
	assert.Zero(t, wait("other inputs", now), "a push of other inputs isn't delayed")
	_, err = b.Wait("object", "inputs", now.Add(defaultBackoffBase))
	assert.NoError(t, err, "waited long enough: nothing to report")
	assert.Zero(t, wait("inputs", now.Add(defaultBackoffBase)))

	b.Failed("object", "inputs", failed, now)
	assert.Equal(t, 2*defaultBackoffBase, wait("inputs", now), "it doubles")
	for range 20 {
		b.Failed("object", "inputs", failed, now)
	}
	assert.Equal(t, defaultBackoffMax, wait("inputs", now), "up to its maximum")

	b.Failed("object", "other inputs", failed, now)
	assert.Equal(t, defaultBackoffBase, wait("other inputs", now), "other inputs start over")

	b.Forget("object")
	assert.Zero(t, wait("other inputs", now), "pushed")
}
