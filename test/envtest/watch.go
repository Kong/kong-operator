package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clientWatchTimeout = 30 * time.Second
)

// setupWatch sets up a watch.Interface for the provided client.ObjectList.
// It is useful if an action needs to be performed between setting up the watch
// and starting to watch for actual events on that watch.
// It returns the watch.Interface and registers its cleanup using t.Cleanup.
func setupWatch[
	TList any,
	TObjectList interface {
		*TList
		client.ObjectList
	},
](
	t *testing.T,
	ctx context.Context,
	cl client.WithWatch,
	opts ...client.ListOption,
) watch.Interface {
	var tlist TList
	var list TObjectList = &tlist
	w, err := cl.Watch(ctx, list, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { w.Stop() })
	return w
}

// watchFor watches for an event of type eventType using the provided watch.Interface.
// It returns when either context is done - and then it marks the test as failed -
// or when the event has been received and predicate returned true.
func watchFor[
	T client.Object,
](
	t *testing.T,
	ctx context.Context,
	w watch.Interface,
	eventType watch.EventType,
	predicate func(T) bool,
	failMsg string,
) T {
	t.Helper()

	ctx, cancel := context.WithTimeout(ctx, clientWatchTimeout)
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
