package helpers

import (
	"context"
	"testing"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchFor watches for an event of type eventType using the provided watch.Interface.
// It returns when either context is done - and then it marks the test as failed -
// or when the event has been received and predicate returned true.
// This is a more generic helper watch function that can watch for both our own CRDs and other resources.
func WatchFor[
	T client.Object,
](
	t *testing.T,
	ctx context.Context,
	w apiwatch.Interface,
	eventType apiwatch.EventType,
	timeout time.Duration,
	predicate func(T) bool,
	failMsg string,
) T {
	t.Helper()

	require.Greater(t, timeout, time.Duration(0), "Must provide a duration greater than 0 as the timeout")

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		obj                   T
		receivedAtLeastOneObj bool
	)
	for found := false; !found; {
		select {
		case <-ctx.Done():
			if receivedAtLeastOneObj {
				require.Failf(t, failMsg, "Last object received:\n%v", pretty.Sprint(obj))
			} else {
				require.Fail(t, failMsg)
			}
		case e := <-w.ResultChan():
			if e.Type != eventType {
				continue
			}
			var ok bool
			obj, ok = e.Object.(T)
			if !ok {
				continue
			}
			receivedAtLeastOneObj = true
			if !predicate(obj) {
				continue
			}
			found = true
		}
	}
	return obj
}
