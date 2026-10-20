package metadata

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

func TestIsRouteOwnerKind(t *testing.T) {
	for _, kind := range []string{"HTTPRoute", "GRPCRoute", "TLSRoute", "TCPRoute", "UDPRoute"} {
		assert.True(t, isRouteOwnerKind(kind), "expected %s to be a route owner kind", kind)
	}
	// Gateway owns its children through owner references and the gateway
	// annotation, not through a hybrid-routes annotation.
	for _, kind := range []string{"Gateway", "KongRoute", ""} {
		assert.False(t, isRouteOwnerKind(kind), "expected %q not to be a route owner kind", kind)
	}
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

	t.Run("gateway root keeps the broad managed-by listing", func(t *testing.T) {
		gw := &types.Gateway{Namespace: "ns", Name: "gw-a"}
		gw.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway",
		})

		// Gateway children carry the gateway annotation and never a hybrid-routes
		// annotation, so the index cannot scope them: an empty indexed listing
		// must not swallow the cleanup listing.
		cert := func(name, gatewayAnnotation string) *configurationv1alpha1.KongCertificate {
			c := &configurationv1alpha1.KongCertificate{
				Namespace: "ns",
				Name:      name,
				Labels:    map[string]string{consts.GatewayOperatorManagedByLabel: "Gateway"},
			}
			if gatewayAnnotation != "" {
				c.Annotations = map[string]string{consts.GatewayOperatorHybridGatewaysAnnotation: gatewayAnnotation}
			}
			return c
		}

		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(cert("gw-a-cert", "ns/gw-a"), cert("gw-b-cert", "ns/gw-b")).
			WithIndex(&configurationv1alpha1.KongCertificate{}, HybridRouteOwnerIndexField, HybridRouteOwnerIndexFunc).
			Build()

		certGVK := configurationv1alpha1.SchemeGroupVersion.WithKind("KongCertificate")
		list, err := ListOwnedChildResources(ctx, cl, gw, certGVK)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"gw-a-cert", "gw-b-cert"}, itemNames(list))
	})

	t.Run("fallback to the live listing without the index still scopes to the root object", func(t *testing.T) {
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ownedByA, ownedByB, shared, unannotated).
			Build()

		list, err := ListOwnedChildResources(ctx, cl, rA, gvk)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"owned-by-a", "shared"}, itemNames(list))
	})
}

// TestListOwnedChildResourcesLive verifies that the live (uncached) listing is
// narrowed to the root object's own children for roots that own their children
// through the hybrid-routes annotations, and stays unscoped for roots that do not
// (a Gateway owns its children through owner references and the gateway
// annotation, so filtering them by a route annotation would drop every child).
func TestListOwnedChildResourcesLive(t *testing.T) {
	var (
		ctx = context.Background()
		s   = objScheme.Get()
		gvk = configurationv1alpha1.SchemeGroupVersion.WithKind("KongRoute")
		rA  = &types.HTTPRoute{Namespace: "ns", Name: "route-a"}
	)
	rA.SetGroupVersionKind(httpRouteGVK)

	ownedByA := kongRouteWithAnnotations("owned-by-a", map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a",
	})
	ownedByB := kongRouteWithAnnotations("owned-by-b", map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-b",
	})
	unannotated := kongRouteWithAnnotations("unannotated", nil)

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ownedByA, ownedByB, unannotated).
		Build()

	t.Run("route root", func(t *testing.T) {
		list, err := ListOwnedChildResourcesLive(ctx, cl, rA, gvk)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"owned-by-a"}, itemNames(list))
	})

	t.Run("gateway root", func(t *testing.T) {
		gw := &types.Gateway{Namespace: "ns", Name: "gw-a"}
		gw.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway",
		})

		certGVK := configurationv1alpha1.SchemeGroupVersion.WithKind("KongCertificate")
		gwCert := &configurationv1alpha1.KongCertificate{
			Namespace: "ns",
			Name:      "gw-a-cert",
			Labels:    map[string]string{consts.GatewayOperatorManagedByLabel: "Gateway"},
		}
		require.NoError(t, cl.Create(ctx, gwCert))

		list, err := ListOwnedChildResourcesLive(ctx, cl, gw, certGVK)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"gw-a-cert"}, itemNames(list),
			"children without a hybrid-routes annotation must not be filtered out for a gateway root")
	})
}

func itemNames(list *unstructured.UnstructuredList) []string {
	out := make([]string, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, list.Items[i].GetName())
	}
	return out
}

// TestIsIndexUnavailableError pins the wording of the two clients that can
// answer the indexed list in this repo, so a wording change fails here instead
// of silently turning every reconcile into a broad live listing.
func TestIsIndexUnavailableError(t *testing.T) {
	// client-go informer cache: "Index with name <field> does not exist", where
	// controller-runtime registered the index under the "field:"-prefixed name.
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	_, err := indexer.ByIndex(cachedIndexFieldName, "HTTPRoute/ns/route-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), cachedIndexFieldName, "client-go no longer names the prefixed index")
	assert.True(t, isIndexUnavailableError(err), "unrecognized client-go wording: %v", err)

	// controller-runtime fake client: "no index with name <field> has been registered".
	err = fake.NewClientBuilder().WithScheme(objScheme.Get()).Build().List(
		context.Background(),
		&configurationv1alpha1.KongRouteList{},
		client.MatchingFields{HybridRouteOwnerIndexField: "HTTPRoute/ns/route-a"},
	)
	require.Error(t, err)
	assert.True(t, isIndexUnavailableError(err), "unrecognized fake client wording: %v", err)

	assert.False(t, isIndexUnavailableError(context.Canceled))
	assert.False(t, isIndexUnavailableError(nil))
}

// TestListOwnedChildResourcesReturnsNonIndexErrors verifies that only an
// unavailable index falls back to the broad live listing: any other failure is
// returned so a broken cache is visible instead of silently slow.
func TestListOwnedChildResourcesReturnsNonIndexErrors(t *testing.T) {
	var (
		ctx      = context.Background()
		gvk      = configurationv1alpha1.SchemeGroupVersion.WithKind("KongRoute")
		rA       = &types.HTTPRoute{Namespace: "ns", Name: "route-a"}
		cacheErr = errors.New("cache failure")
	)
	rA.SetGroupVersionKind(httpRouteGVK)

	cl := fake.NewClientBuilder().
		WithScheme(objScheme.Get()).
		WithObjects(kongRouteWithAnnotations("owned-by-a", map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/route-a",
		})).
		WithIndex(&configurationv1alpha1.KongRoute{}, HybridRouteOwnerIndexField, HybridRouteOwnerIndexFunc).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*configurationv1alpha1.KongRouteList); ok {
					return cacheErr
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()

	_, err := ListOwnedChildResources(ctx, cl, rA, gvk)
	require.ErrorIs(t, err, cacheErr)
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
