package metadata

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/types"
	objScheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var httpRouteGVK = schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}

func kongRouteWithAnnotations(name string, annotations map[string]string) *configurationv1alpha1.KongRoute {
	r := &configurationv1alpha1.KongRoute{
		Namespace:   "ns",
		Name:        name,
		Annotations: annotations,
		Labels:      map[string]string{consts.GatewayOperatorManagedByLabel: "HTTPRoute"},
	}
	r.SetGroupVersionKind(configurationv1alpha1.SchemeGroupVersion.WithKind("KongRoute"))
	return r
}

func TestHybridRouteOwnerIndexFunc(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name:     "no annotations",
			expected: nil,
		},
		{
			name: "http route annotation",
			annotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a, ns/route-b",
			},
			expected: []string{"HTTPRoute/ns/route-a", "HTTPRoute/ns/route-b"},
		},
		{
			name: "multiple route kinds",
			annotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a",
				consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation: "ns/route-c",
			},
			expected: []string{"HTTPRoute/ns/route-a", "GRPCRoute/ns/route-c"},
		},
		{
			name: "empty and malformed entries are skipped",
			annotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTCPRouteAnnotation: " , ns/route-a ,,",
			},
			expected: []string{"TCPRoute/ns/route-a"},
		},
		{
			name: "unrelated annotations are ignored",
			annotations: map[string]string{
				"some-other-annotation": "ns/route-a",
			},
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := kongRouteWithAnnotations("child", tt.annotations)
			assert.ElementsMatch(t, tt.expected, HybridRouteOwnerIndexFunc(obj))
		})
	}
}

func TestHybridRouteOwnerIndexKey(t *testing.T) {
	route := &types.HTTPRoute{
		Namespace: "ns", Name: "route-a",
	}
	route.SetGroupVersionKind(httpRouteGVK)
	assert.Equal(t, "HTTPRoute/ns/route-a", HybridRouteOwnerIndexKey(route))
}

// TestListOwnedChildResources verifies the indexed fast path (only the root
// object's children are returned, served from a client with the field index
// registered) and the legacy fallback (broad managed-by label listing) for
// clients without the index.
func TestListOwnedChildResources(t *testing.T) {
	var (
		ctx = context.Background()
		s   = objScheme.Get()
		gvk = configurationv1alpha1.SchemeGroupVersion.WithKind("KongRoute")
		rA  = &types.HTTPRoute{Namespace: "ns", Name: "route-a"}
		rB  = &types.HTTPRoute{Namespace: "ns", Name: "route-b"}
	)
	rA.SetGroupVersionKind(httpRouteGVK)
	rB.SetGroupVersionKind(httpRouteGVK)

	ownedByA := kongRouteWithAnnotations("owned-by-a", map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a",
	})
	ownedByB := kongRouteWithAnnotations("owned-by-b", map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-b",
	})
	shared := kongRouteWithAnnotations("shared", map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a,ns/route-b",
	})
	unannotated := kongRouteWithAnnotations("unannotated", nil)

	t.Run("indexed fast path returns only the root object's children", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ownedByA, ownedByB, shared, unannotated).
			WithIndex(&configurationv1alpha1.KongRoute{}, HybridRouteOwnerIndexField, HybridRouteOwnerIndexFunc).
			Build()

		list, err := ListOwnedChildResources(ctx, cl, rA, gvk)
		require.NoError(t, err)
		names := itemNames(list)
		assert.ElementsMatch(t, []string{"owned-by-a", "shared"}, names)

		list, err = ListOwnedChildResources(ctx, cl, rB, gvk)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"owned-by-b", "shared"}, itemNames(list))
	})

	t.Run("fallback to broad managed-by label listing without the index", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ownedByA, ownedByB, shared, unannotated).
			Build()

		list, err := ListOwnedChildResources(ctx, cl, rA, gvk)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"owned-by-a", "owned-by-b", "shared", "unannotated"}, itemNames(list))
	})
}

func itemNames(list *unstructured.UnstructuredList) []string {
	out := make([]string, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, list.Items[i].GetName())
	}
	return out
}

// stubIndexer is a minimal client.FieldIndexer that reports an indexer
// conflict for already-registered type/field pairs, like the informer cache
// and the fake client do.
type stubIndexer struct {
	registered map[string]bool
}

func (s *stubIndexer) IndexField(_ context.Context, obj client.Object, field string, _ client.IndexerFunc) error {
	key := fmt.Sprintf("%T/%s", obj, field)
	if s.registered[key] {
		return fmt.Errorf("indexer conflict: %s", field)
	}
	s.registered[key] = true
	return nil
}

// TestRegisterHybridRouteOwnerIndexesIdempotent verifies that registering the
// index multiple times (as happens when several root-object controllers share
// one manager) does not fail.
func TestRegisterHybridRouteOwnerIndexesIdempotent(t *testing.T) {
	idx := &stubIndexer{registered: map[string]bool{}}
	require.NoError(t, RegisterHybridRouteOwnerIndexes(context.Background(), idx))
	require.NoError(t, RegisterHybridRouteOwnerIndexes(context.Background(), idx))
	assert.Len(t, idx.registered, len(hybridRouteOwnerChildTypes))
}
