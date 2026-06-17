package metadata

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var kongUpstreamGVK = schema.GroupVersionKind{
	Group:   "configuration.konghq.com",
	Version: "v1alpha1",
	Kind:    "KongUpstream",
}

func newKongUpstreamUnstructured(annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kongUpstreamGVK)
	obj.SetName("up-1")
	obj.SetNamespace("ns")
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func TestEnsureRouteInAnnotation(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-a",
			Namespace: "ns",
		},
	}
	routeKey := client.ObjectKeyFromObject(route).String()
	key := client.ObjectKey{Namespace: "ns", Name: "up-1"}

	tests := []struct {
		name     string
		existing *unstructured.Unstructured
		wantAnno string // expected hybrid-routes HTTPRoute annotation value
	}{
		{
			name:     "adds route when annotation is empty",
			existing: newKongUpstreamUnstructured(nil),
			wantAnno: routeKey,
		},
		{
			name: "appends route preserving other routes",
			existing: newKongUpstreamUnstructured(map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/other-route",
			}),
			wantAnno: "ns/other-route," + routeKey,
		},
		{
			name: "no-op when route already present",
			existing: newKongUpstreamUnstructured(map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: routeKey,
			}),
			wantAnno: routeKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tt.existing).Build()
			am := NewAnnotationManager(logr.Discard())

			err := am.EnsureRouteInAnnotation(context.Background(), cl, kongUpstreamGVK, key, route)
			require.NoError(t, err)

			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(kongUpstreamGVK)
			require.NoError(t, cl.Get(context.Background(), key, got))
			assert.Equal(t, tt.wantAnno, got.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
		})
	}
}

func TestEnsureRouteInAnnotation_NoopWhenObjectMissing(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-a",
			Namespace: "ns",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	am := NewAnnotationManager(logr.Discard())

	// Object does not exist yet: should be a no-op (it will be tracked once created).
	err := am.EnsureRouteInAnnotation(
		context.Background(), cl, kongUpstreamGVK, client.ObjectKey{Namespace: "ns", Name: "missing"}, route,
	)
	require.NoError(t, err)
}

func TestEnsureRouteInAnnotation_SkipsUntrackedRouteKind(t *testing.T) {
	// A Gateway is not tracked via the hybrid-routes annotation; the helper must be a no-op and
	// must not even attempt to mutate the object.
	gateway := &gwtypes.Gateway{
		TypeMeta: metav1.TypeMeta{Kind: "Gateway", APIVersion: "gateway.networking.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "ns",
		},
	}
	existing := newKongUpstreamUnstructured(nil)
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(existing).Build()
	am := NewAnnotationManager(logr.Discard())

	err := am.EnsureRouteInAnnotation(
		context.Background(), cl, kongUpstreamGVK, client.ObjectKey{Namespace: "ns", Name: "up-1"}, gateway,
	)
	require.NoError(t, err)

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(kongUpstreamGVK)
	require.NoError(t, cl.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "up-1"}, got))
	assert.Empty(t, got.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
}

func TestEnsureRouteInAnnotation_ReturnsConflictForRequeue(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-a",
			Namespace: "ns",
		},
	}
	key := client.ObjectKey{Namespace: "ns", Name: "up-1"}

	existing := newKongUpstreamUnstructured(map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/other-route",
	})

	// Fail the Patch with a Conflict (simulating a concurrent writer). The helper should surface
	// the conflict so the reconciler requeues instead of retrying inside this API call path.
	var patchCalls int
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchCalls++
				if patchCalls == 1 {
					return apierrors.NewConflict(schema.GroupResource{Group: kongUpstreamGVK.Group, Resource: "kongupstreams"}, obj.GetName(), assert.AnError)
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	am := NewAnnotationManager(logr.Discard())
	err := am.EnsureRouteInAnnotation(context.Background(), cl, kongUpstreamGVK, key, route)
	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err))
	assert.Equal(t, 1, patchCalls, "expected a single patch attempt and controller-level requeue")

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(kongUpstreamGVK)
	require.NoError(t, cl.Get(context.Background(), key, got))
	assert.Equal(t, "ns/other-route", got.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
}
