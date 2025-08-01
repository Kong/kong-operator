package status

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestQueue(t *testing.T) {
	t.Log("creating a status queue")
	q := NewQueue()

	t.Log("generating Kubernetes objects to emit events for the queue")
	ing1 := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "ingress-test-1",
		},
	}
	ing2 := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "ingress-test-1",
		},
	}

	t.Log("initializing kubernetes objects (this would normally be done by api client)")
	ing1.SetGroupVersionKind(ingGVK)
	ing2.SetGroupVersionKind(ingGVK)

	t.Log("verifying that events can be subscribed to for new object kinds")
	ingCH := q.Subscribe(ing1.GroupVersionKind())
	assert.Len(t, q.subscriptions[ing1.GroupVersionKind().String()], 1, "internally a single channel should be created for the object kind")
	t.Logf("%+v", q.subscriptions)
	assert.Empty(t, ingCH, "the underlying channel should be empty")

	t.Log("verifying an object can have an event published for it")
	q.Publish(ing1)
	assert.Len(t, q.subscriptions[ing1.GroupVersionKind().String()], 1, "a channel was already created for the consumer: no more should be created")
	assert.Len(t, ingCH, 1, "the underlying channel should now contain one event")

	t.Log("verifying a published event can be consumed by the consumer")
	assert.Equal(t, event.GenericEvent{Object: ing1}, <-ingCH)
	assert.Empty(t, ingCH, "the event should be consumed")

	t.Log("verifying publishing different named objects for kinds that have already been seen")
	q.Publish(ing2)
	assert.Len(t, q.subscriptions[ing1.GroupVersionKind().String()], 1, "a channel was already created for the object kind: no more should be created")
	assert.Len(t, ingCH, 1, "the underlying channel should now contain one event")

	t.Log("verifying that publishing different named objects for kinds that have already been seen")
	q.Publish(ing2)
	assert.Len(t, q.subscriptions[ing1.GroupVersionKind().String()], 1, "a channel was already created for the object kind: no more should be created")
	assert.Len(t, ingCH, 2, "the underlying channel should now contain two events")

	t.Log("verifying that multiple events can be submitted for the same object")
	q.Publish(ing1)
	q.Publish(ing2)
	q.Publish(ing2)
	q.Publish(ing1)
	q.Publish(ing1)
	assert.Len(t, q.subscriptions, 1)
	assert.Len(t, ingCH, 7)

	t.Log("verifying that all objects can be consumed and the queue can be drained")
	for range 7 {
		assert.Equal(t, ingGVK, (<-ingCH).Object.GetObjectKind().GroupVersionKind())
	}
	assert.Len(t, q.subscriptions, 1)
	assert.Empty(t, ingCH)

	t.Log("verifying that multiple consumers can be subscribed to the same object kind and receive events")
	ingCH2 := q.Subscribe(ing1.GroupVersionKind())
	require.Len(t, q.subscriptions[ing1.GroupVersionKind().String()], 2, "a second channel should have been created for the object kind")
	q.Publish(ing1)
	require.Len(t, ingCH, 1, "the first consumer should have received an event")
	require.Len(t, ingCH2, 1, "the second consumer should have received an event")
	assert.Equal(t, event.GenericEvent{Object: ing1}, <-ingCH)
	assert.Equal(t, event.GenericEvent{Object: ing1}, <-ingCH2)
}

// the GVKs for objects need to be initialized manually in the unit testing
// case as this would normally be done by the API and client for real objects.
var (
	ingGVK = schema.GroupVersionKind{
		Group:   "networking.k8s.io",
		Version: "v1",
		Kind:    "Ingress",
	}
)

func TestQueuePublish(t *testing.T) {
	const testBufferSize = 1
	testObj := &netv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: corev1.NamespaceDefault,
			Name:      "ingress-test-1",
		},
	}

	// shouldCompleteAlmostImmediately is a helper function that runs a given action
	// in a goroutine and verifies that it completes within a second.
	shouldCompleteAlmostImmediately := func(t *testing.T, action func()) {
		done := make(chan struct{})
		go func() {
			action()
			close(done)
		}()
		select {
		case <-done:
			return
		case <-time.After(1 * time.Second):
			t.Fatal("action did not complete in time")
		}
	}

	t.Run("does not block when no subscription exists", func(t *testing.T) {
		q := NewQueue(WithBufferSize(testBufferSize))

		shouldCompleteAlmostImmediately(t, func() {
			// Publish more events than the buffer size and expect no block.
			for range testBufferSize + 1 {
				q.Publish(testObj)
			}
		})
	})

	t.Run("blocks when subscription exists and buffer is full", func(t *testing.T) {
		q := NewQueue(WithBufferSize(testBufferSize))
		sub := q.Subscribe(testObj.GroupVersionKind())

		shouldCompleteAlmostImmediately(t, func() {
			// Publish exactly the number of events that fit in the buffer. Expect no block.
			// This is to ensure that the buffer is full.
			for range testBufferSize {
				q.Publish(testObj)
			}
		})

		require.Len(t, sub, testBufferSize, "the channel should be full")

		published := make(chan struct{})
		go func() {
			q.Publish(testObj)
			close(published)
		}()

		select {
		case <-published:
			t.Fatal("the Publish goroutine should be blocked")
		case <-sub:
			// Consume one event from the channel to unblock the Publish goroutine.
		}

		select {
		case <-time.After(1 * time.Second):
			t.Fatal("the Publish goroutine should have completed, timeout")
		case <-published:
		}
		require.Len(t, sub, testBufferSize, "the channel should be full again")
	})
}
