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

func TestOwnedObjectPreDeleteHookReReadsStaleObject(t *testing.T) {
	const otherFinalizer = "example.com/other"

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(svc).
		Build()

	// Cached copy read before another controller updates the object.
	stale := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), stale))

	// Another controller adds a finalizer and a label in the meantime.
	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	current.Finalizers = append(current.Finalizers, otherFinalizer)
	if current.Labels == nil {
		current.Labels = map[string]string{}
	}
	current.Labels["updated"] = "true"
	require.NoError(t, cl.Update(t.Context(), current))

	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, stale))

	current = &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	require.Equal(t, []string{otherFinalizer}, current.Finalizers)
	require.Equal(t, "true", current.Labels["updated"])
}

func TestOwnedObjectPreDeleteHookRetriesOnConflict(t *testing.T) {
	var updateCalls int
	fakeClient := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				updateCalls++
				if updateCalls == 1 {
					return apierrors.NewConflict(
						schema.GroupResource{Resource: "services"},
						obj.GetName(),
						errors.New("the object has been modified"),
					)
				}
				return cl.Update(ctx, obj, opts...)
			},
		}).
		Build()

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), fakeClient, svc))
	require.Equal(t, 2, updateCalls)

	current := &corev1.Service{}
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(svc), current))
	require.Empty(t, current.Finalizers)
}

func TestOwnedObjectPreDeleteHookIgnoresDeletedObject(t *testing.T) {
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		Build()

	svc := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, svc))
}

func TestOwnedObjectPreDeleteHookDoesNotTouchReplacementObject(t *testing.T) {
	replacement := newServiceWithFinalizers("uid-2", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	cl := fake.NewClientBuilder().
		WithScheme(newPreDeleteHookScheme(t)).
		WithObjects(replacement).
		Build()

	// A stale copy of the object that was deleted and re-created with the same name.
	stale := newServiceWithFinalizers("uid-1", consts.DataPlaneOwnedWaitForOwnerFinalizer)
	require.NoError(t, OwnedObjectPreDeleteHook(t.Context(), cl, stale))

	current := &corev1.Service{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(replacement), current))
	require.Equal(t, "uid-2", string(current.UID))
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
