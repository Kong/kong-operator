package envtest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clientWatchTimeout = 45 * time.Second
)

// watch is a wrapper around watch.Interface.
// It is useful to provide a type-safe way to access the watch.Interface so that
// the callers do not use an invalid watches when using watchFor().
type watch[T any] struct {
	w apiwatch.Interface
}

// WatchI returns the wrapped watch.Interface.
func (w watch[T]) WatchI() apiwatch.Interface {
	return w.w
}

// setupWatch sets up a watch.Interface for the provided client.ObjectList.
// It is useful if an action needs to be performed between setting up the watch
// and starting to watch for actual events on that watch.
// It returns the watch.Interface and registers its cleanup using t.Cleanup.
func setupWatch[
	TList interface {
		GetItems() []T
	},
	TObjectList interface {
		*TList
		client.ObjectList
	},
	T any,
	TPtr interface {
		*T
		client.Object
	},
](
	t *testing.T,
	ctx context.Context,
	cl client.WithWatch,
	opts ...client.ListOption,
) watch[TPtr] {
	t.Helper()
	var (
		tlist   TList
		list    TObjectList = &tlist
		strType             = strings.TrimSuffix(fmt.Sprintf("%T", list), "List")
	)

	t.Logf("Setting up a watch for %s events", strType)

	w, err := cl.Watch(ctx, list, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { w.Stop() })
	return watch[TPtr]{
		w: w,
	}
}

// watchFor watches for an event of type eventType using the provided watch.Interface.
// It returns when either context is done - and then it marks the test as failed -
// or when the event has been received and predicate returned true.
func watchFor[
	T client.Object,
](
	t *testing.T,
	ctx context.Context,
	w watch[T],
	eventType apiwatch.EventType,
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
		case e := <-w.WatchI().ResultChan():
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
