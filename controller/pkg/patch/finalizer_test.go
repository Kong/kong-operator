package patch

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestWithoutFinalizerStaleCopyDoesNotReAddOtherFinalizers verifies that removing
// a finalizer with a stale cached copy does not re-add finalizers that other
// controllers removed in the meantime. The optimistic lock makes the stale patch
// fail with a conflict (requeue) instead of silently resurrecting the sibling
// finalizer - which, for objects being deleted, the API server would reject with
// "no new finalizers can be added if the object is being deleted".
func TestWithoutFinalizerStaleCopyDoesNotReAddOtherFinalizers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const (
		ownFinalizer     = "example.com/own"
		siblingFinalizer = "example.com/sibling"
	)

	newSecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test",
				Namespace:  "default",
				Finalizers: []string{ownFinalizer, siblingFinalizer},
			},
		}
	}

	t.Run("stale copy does not re-add sibling finalizer", func(t *testing.T) {
		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme).
			WithObjects(newSecret()).
			Build()

		// Stale cached copy of the object (fetched before the concurrent update,
		// so it carries the older resourceVersion).
		cached := newSecret()
		require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(cached), cached))

		// Another controller removes its finalizer server-side in the meantime.
		current := newSecret()
		require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(current), current))
		current.Finalizers = []string{ownFinalizer}
		require.NoError(t, fakeClient.Update(t.Context(), current))

		// Removing our finalizer from the stale copy must not re-add the sibling.
		patched, res, err := WithoutFinalizer(t.Context(), fakeClient, cached, ownFinalizer)
		require.NoError(t, err)
		require.False(t, patched)
		// Conflict outcome: request is requeued with a fresh copy.
		require.NotZero(t, res)

		require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(current), current))
		require.Equal(t, []string{ownFinalizer}, current.Finalizers)
	})

	t.Run("fresh copy removes only own finalizer", func(t *testing.T) {
		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme).
			WithObjects(newSecret()).
			Build()

		cached := newSecret()
		require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(cached), cached))

		patched, res, err := WithoutFinalizer(t.Context(), fakeClient, cached, ownFinalizer)
		require.NoError(t, err)
		require.True(t, patched)
		require.Zero(t, res)

		current := newSecret()
		require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(current), current))
		require.Equal(t, []string{siblingFinalizer}, current.Finalizers)
	})
}

// TestWithFinalizerStaleCopyReturnsConflict verifies that adding a finalizer with
// a stale cached copy fails with a conflict (requeue) instead of being applied
// on top of an object state the copy no longer matches.
func TestWithFinalizerStaleCopyReturnsConflict(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	const finalizer = "example.com/own"

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}).
		Build()

	// Stale cached copy of the object (fetched before the concurrent update,
	// so it carries the older resourceVersion).
	cached := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(cached), cached))

	// Another controller updates the object server-side in the meantime.
	current := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(current), current))
	current.Finalizers = []string{"example.com/sibling"}
	require.NoError(t, fakeClient.Update(t.Context(), current))

	patched, res, err := WithFinalizer(t.Context(), fakeClient, cached, finalizer)
	require.NoError(t, err)
	require.False(t, patched)
	// Conflict outcome: request is requeued with a fresh copy.
	require.NotZero(t, res)

	// The finalizer must not have been added to the server-side object.
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(current), current))
	require.Equal(t, []string{"example.com/sibling"}, current.Finalizers)
}
