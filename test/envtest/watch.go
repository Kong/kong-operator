package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// watchFor watches for an event of type eventType using the provided watch.Interface.
// It returns when either context is done - and then it marks the test as failed -
// or when the event has been received and predicate returned true.
func watchFor[
	T metav1.Object,
](
	t *testing.T,
	ctx context.Context,
	w watch.Interface,
	eventType watch.EventType,
	predicate func(T) bool,
	failMsg string,
) T {
	t.Helper()

	var ret T
	for found := false; !found; {
		select {
		case <-ctx.Done():
			require.Fail(t, failMsg)
		case e := <-w.ResultChan():
			if e.Type != eventType {
				continue
			}
			obj, ok := e.Object.(T)
			if !ok {
				continue
			}
			if !predicate(obj) {
				continue
			}
			found = true
			ret = obj
		}
	}
	return ret
}
