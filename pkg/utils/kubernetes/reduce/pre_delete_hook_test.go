package reduce

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

func newServiceWithFinalizers(uid types.UID, finalizers ...string) *corev1.Service {
	return &corev1.Service{
		Name:       "test-service",
		Namespace:  "default",
		UID:        uid,
		Finalizers: finalizers,
	}
}

func newPreDeleteHookScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	return scheme
}

func TestOwnedObjectPreDeleteHookRemovesOnlyOwnFinalizer(t *testing.T) {
	const otherFinalizer = "example.com/other"

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer, otherFinalizer)
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(svc).
		Build()

	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, svc))

	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	require.Equal(t, []string{otherFinalizer}, current.Finalizers)
}

func TestOwnedObjectPreDeleteHookReReadsStaleObjectIntoZeroTarget(t *testing.T) {
	const otherFinalizer = "example.com/other"

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	svc.Labels = map[string]string{"stale": "true"}

	var sawPrePopulatedGetTarget bool
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(svc).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if len(obj.GetFinalizers()) > 0 || len(obj.GetLabels()) > 0 || obj.GetName() != "" {
					sawPrePopulatedGetTarget = true
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	// Cached copy read before another controller updates the object.
	stale := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), stale))

	// Another controller removes the stale label and adds a finalizer and a new label.
	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	current.Finalizers = append(current.Finalizers, otherFinalizer)
	delete(current.Labels, "stale")
	current.Labels["updated"] = "true"
	require.NoError(t, cl.Update(t.Context(), current))

	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, stale))
	require.False(t, sawPrePopulatedGetTarget, "cl.Get must decode into a zero-valued object")

	current = &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	require.Equal(t, []string{otherFinalizer}, current.Finalizers)
	require.Equal(t, map[string]string{"updated": "true"}, current.Labels)
}

func TestOwnedObjectPreDeleteHookRetriesOnConflict(t *testing.T) {
	const otherFinalizer = "example.com/other"

	var getCalls, patchCalls int
	fakeClient := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				getCalls++
				return cl.Get(ctx, key, obj, opts...)
			},
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchCalls++
				if patchCalls == 1 {
					// Simulate another controller updating the stored object right before our
					// first optimistic-lock patch lands, triggering a real resourceVersion conflict.
					stored := &corev1.Service{}
					if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), stored); err != nil {
						return err
					}
					stored.Finalizers = append(stored.Finalizers, otherFinalizer)
					stored.Labels = map[string]string{"concurrent": "true"}
					if err := cl.Update(ctx, stored); err != nil {
						return err
					}
				}
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), fakeClient, svc))
	require.Equal(t, 2, getCalls)
	require.Equal(t, 2, patchCalls)

	current := &corev1.Service{}
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	require.Equal(t, []string{otherFinalizer}, current.Finalizers)
	require.Equal(t, "true", current.Labels["concurrent"])
}

func TestOwnedObjectPreDeleteHookStopsWhenFinalizerAlreadyRemoved(t *testing.T) {
	const otherFinalizer = "example.com/other"

	var patchCalls int
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(newServiceWithFinalizers("uid-1", otherFinalizer)).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchCalls++
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	stale := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer, otherFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, stale))
	require.Zero(t, patchCalls)

	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(stale), current))
	require.Equal(t, []string{otherFinalizer}, current.Finalizers)
}

func TestOwnedObjectPreDeleteHookErrorHandling(t *testing.T) {
	t.Run("exhausted conflict preserves conflict error", func(t *testing.T) {
		var patchCalls int
		cl := fake.NewClientBuilder().
			WithScheme(newPreDeleteHookScheme(t)).
			WithObjects(newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
					patchCalls++
					return apierrors.NewConflict(
						schema.GroupResource{Resource: "services"},
						obj.GetName(),
						errors.New("the object has been modified"),
					)
				},
			}).
			Build()

		svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
		err := OwnedObjectPreDeleteHook(t.Context(), cl, svc)
		require.Error(t, err)
		require.True(t, apierrors.IsConflict(err))
		require.Equal(t, retry.DefaultRetry.Steps, patchCalls)
	})

	t.Run("non-conflict error fails immediately without retry", func(t *testing.T) {
		var patchCalls int
		cl := fake.NewClientBuilder().
			WithScheme(newPreDeleteHookScheme(t)).
			WithObjects(newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
					patchCalls++
					return apierrors.NewForbidden(
						schema.GroupResource{Resource: "services"},
						obj.GetName(),
						errors.New("forbidden"),
					)
				},
			}).
			Build()

		svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
		err := OwnedObjectPreDeleteHook(t.Context(), cl, svc)
		require.Error(t, err)
		require.True(t, apierrors.IsForbidden(err))
		require.Equal(t, 1, patchCalls)
	})
}

func TestOwnedObjectPreDeleteHookIgnoresDeletedObject(t *testing.T) {
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		Build()

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, svc))
}

func TestOwnedObjectPreDeleteHookDoesNotTouchReplacementObject(t *testing.T) {
	var patchCalls int
	replacement := newServiceWithFinalizers("uid-2", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(replacement).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchCalls++
				return cl.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	// A stale copy of the object that was deleted and re-created with the same name.
	stale := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	err := ReduceServicesByName(t.Context(), cl, []corev1.Service{*stale}, "other-service", OwnedObjectPreDeleteHook)
	require.Error(t, err)
	require.True(t, apierrors.IsConflict(err))
	require.Zero(t, patchCalls)

	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(replacement), current))
	require.Equal(t, "uid-2", string(current.UID))
	require.True(t, current.DeletionTimestamp.IsZero(), "replacement object must not be marked for deletion")
	require.Equal(t, []string{consts.DataPlaneOwnedWaitForOwnerFinalizer}, current.Finalizers)
}

func TestOwnedObjectPreDeleteHookNoFinalizer(t *testing.T) {
	var getCalls int
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				getCalls++
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	svc := newServiceWithFinalizers("uid-1")
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, svc))
	require.Zero(t, getCalls, "no API request is needed when the finalizer is absent")
}
