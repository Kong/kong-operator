package asserts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Never asserts that the provided conditionFunc never returns true
// within the specified waitTime, checking the condition every tickTime.
// If the conditionFunc returns true at any point, the test fails with
// the provided message and arguments.
// The conditionFunc receives a [context.Context] which is derived from
// the [testing.T] context, allowing for cancellation and timeouts.
// The conditionFunc is run immediately upon entering the function,
// in the same goroutine as the caller, thus it's a blocking call.
func Never(
	t *testing.T,
	conditionFunc func(ctx context.Context) bool,
	waitTime,
	tickTime time.Duration,
	msgAndArgs ...any,
) {
	t.Helper()
	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	if len(msgAndArgs) == 0 {
		msgAndArgs = []any{""}
	}

	for {
		tickTimer := time.NewTimer(tickTime)
		defer tickTimer.Stop()

		if conditionFunc(t.Context()) {
			require.Fail(t, "Condition met unexpectedly: "+msgAndArgs[0].(string), msgAndArgs[1:]...)
		}

		select {
		case <-tickTimer.C:
		case <-t.Context().Done():
			return
		case <-timer.C:
			return
		}
	}
}
