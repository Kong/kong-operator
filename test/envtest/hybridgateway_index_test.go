package envtest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// TestHybridRouteOwnerIndexCachedListing verifies, against a real (informer-based)
// cached client, that the hybrid-route owner field index scopes child listings to
// a single root object: children annotated for other routes are not returned.
// All children here share the roots' namespace; the informer cache's handling of
// a child in a different namespace (e.g. KongReferenceGrant, created in the
// Secret's namespace) is not covered.
func TestHybridRouteOwnerIndexCachedListing(t *testing.T) {
	ctx := t.Context()
	s := scheme.Get()
	envcfg, ns := Setup(t, ctx, s)

	mgr, _ := NewManager(t, ctx, envcfg, s)
	require.NoError(t, metadata.RegisterHybridRouteOwnerIndexes(ctx, mgr.GetFieldIndexer()))
	// Registering twice mirrors the root-object controllers sharing one manager.
	// This pins the wording of the conflict error the real manager field indexer
	// returns, so a change in it fails here instead of every controller setup
	// failing at manager startup.
	require.NoError(t, metadata.RegisterHybridRouteOwnerIndexes(ctx, mgr.GetFieldIndexer()))
	mgrCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() { _ = mgr.Start(mgrCtx) }()
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx), "cache did not sync")

	// Create children directly against the API server (bypassing the cache).
	direct, err := ctrlclient.New(envcfg, ctrlclient.Options{Scheme: s})
	require.NoError(t, err)

	kongRouteGVK := configurationv1alpha1.SchemeGroupVersion.WithKind("KongRoute")
	child := func(name, owners string) *configurationv1alpha1.KongRoute {
		return &configurationv1alpha1.KongRoute{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      name,
				Annotations: map[string]string{
					consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: owners,
				},
			},
		}
	}
	require.NoError(t, direct.Create(ctx, child("owned-by-a", ns.Name+"/route-a")))
	require.NoError(t, direct.Create(ctx, child("owned-by-b", ns.Name+"/route-b")))
	require.NoError(t, direct.Create(ctx, child("shared", ns.Name+"/route-a,"+ns.Name+"/route-b")))
	require.NoError(t, direct.Create(ctx, child("unannotated", "")))

	root := func(name string) *gwtypes.HTTPRoute {
		r := &gwtypes.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: name}}
		r.SetGroupVersionKind(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"})
		return r
	}
	routeA := root("route-a")
	routeB := root("route-b")

	// Wait for the informer cache to pick up the children, then assert the
	// indexed listing returns exactly the root object's own children.
	assert.Eventually(t, func() bool {
		listA, err := metadata.ListOwnedChildResources(ctx, mgr.GetClient(), routeA, kongRouteGVK)
		if err != nil {
			t.Logf("failed to list children of route-a: %v", err)
			return false
		}
		listB, err := metadata.ListOwnedChildResources(ctx, mgr.GetClient(), routeB, kongRouteGVK)
		if err != nil {
			t.Logf("failed to list children of route-b: %v", err)
			return false
		}
		return assert.ObjectsAreEqual([]string{"owned-by-a", "shared"}, namesOf(listA)) &&
			assert.ObjectsAreEqual([]string{"owned-by-b", "shared"}, namesOf(listB))
	}, 60*time.Second, 200*time.Millisecond)
}

// namesOf returns the sorted names of the list items. The informer returns
// index matches in map order, so sorting makes the result comparable.
func namesOf(list *unstructured.UnstructuredList) []string {
	out := make([]string, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, list.Items[i].GetName())
	}
	slices.Sort(out)
	return out
}
