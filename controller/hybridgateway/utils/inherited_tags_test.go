package utils

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

const tagsAnnotation = "konghq.com/tags"

func tagsGateway(namespace, name, gatewayClassName, tags string) *gwtypes.Gateway {
	gw := &gwtypes.Gateway{Name: name, Namespace: namespace}
	if tags != "" {
		gw.Annotations = map[string]string{tagsAnnotation: tags}
	}
	gw.Spec.GatewayClassName = gwtypes.ObjectName(gatewayClassName)
	return gw
}

func tagsGatewayClass(name, tags string) *gwtypes.GatewayClass {
	gwc := &gwtypes.GatewayClass{Name: name}
	if tags != "" {
		gwc.Annotations = map[string]string{tagsAnnotation: tags}
	}
	return gwc
}

// tagsHTTPRoute builds an HTTPRoute in the "ns" namespace whose parentRefs point at the given
// Gateway names in that same namespace, each accepted by this controller. Translation only ever
// runs on a route whose status is already settled, so the fixture carries the status entries
// UpdateRouteStatus would have written.
func tagsHTTPRoute(name string, parents ...string) *gwtypes.HTTPRoute {
	route := &gwtypes.HTTPRoute{Name: name, Namespace: "ns"}
	for _, parent := range parents {
		route.Spec.ParentRefs = append(route.Spec.ParentRefs, gwtypes.ParentReference{
			Name: gwtypes.ObjectName(parent),
		})
	}
	return withAcceptedParents(route, parents...)
}

// withAcceptedParents adds a RouteParentStatus owned by this controller for each named parent.
func withAcceptedParents(route *gwtypes.HTTPRoute, parents ...string) *gwtypes.HTTPRoute {
	for _, parent := range parents {
		route.Status.Parents = append(route.Status.Parents, gwtypes.RouteParentStatus{
			ParentRef:      gwtypes.ParentReference{Name: gwtypes.ObjectName(parent)},
			ControllerName: gwtypes.GatewayController(vars.ControllerName()),
		})
	}
	return route
}

func TestMergeTags(t *testing.T) {
	longTag := strings.Repeat("x", maxTagLength+10)

	tests := []struct {
		name     string
		groups   [][]string
		expected []string
	}{
		{
			name:     "no groups yields nil",
			expected: nil,
		},
		{
			name:     "only empty groups yields nil rather than an empty slice",
			groups:   [][]string{nil, {}, {"", "  "}},
			expected: nil,
		},
		{
			name:     "single group is passed through",
			groups:   [][]string{{"a", "b"}},
			expected: []string{"a", "b"},
		},
		{
			name:     "group order is preserved",
			groups:   [][]string{{"entity"}, {"gateway"}, {"class"}},
			expected: []string{"entity", "gateway", "class"},
		},
		{
			name:     "whitespace is trimmed and empties dropped",
			groups:   [][]string{{" a", "b ", "", "  "}},
			expected: []string{"a", "b"},
		},
		{
			name:     "duplicates are removed across groups, first occurrence wins",
			groups:   [][]string{{"a", "shared"}, {"shared", "b"}, {"a"}},
			expected: []string{"a", "shared", "b"},
		},
		{
			name:     "duplicates that differ only by whitespace collapse",
			groups:   [][]string{{"a"}, {" a "}},
			expected: []string{"a"},
		},
		{
			name:     "over-long tags are truncated to the CRD limit",
			groups:   [][]string{{longTag}},
			expected: []string{strings.Repeat("x", maxTagLength)},
		},
		{
			name:     "tags with reserved prefixes are dropped from every group",
			groups:   [][]string{{"entity", "k8s-uid:1234"}, {" k8s-name:other ", "gateway"}, {"managed-by:someone-else", "class"}},
			expected: []string{"entity", "gateway", "class"},
		},
		{
			name:     "only reserved tags yield nil",
			groups:   [][]string{{"k8s-uid:1234"}, {"k8s-kind:Service"}},
			expected: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, MergeTags(logr.Discard(), tc.groups...))
		})
	}

	t.Run("exactly MaxTags are kept untouched", func(t *testing.T) {
		var tags []string
		for i := range MaxTags {
			tags = append(tags, fmt.Sprintf("tag-%02d", i))
		}
		assert.Equal(t, tags, MergeTags(logr.Discard(), tags))
	})

	t.Run("reserved tags do not count towards MaxTags", func(t *testing.T) {
		var tags, withReserved []string
		for i := range MaxTags {
			tag := fmt.Sprintf("tag-%02d", i)
			tags = append(tags, tag)
			withReserved = append(withReserved, fmt.Sprintf("k8s-uid:%02d", i), tag)
		}
		assert.Equal(t, tags, MergeTags(logr.Discard(), withReserved))
	})

	t.Run("over budget drops from the end, so the least specific group loses first", func(t *testing.T) {
		var entityTags []string
		for i := range MaxTags - 2 {
			entityTags = append(entityTags, fmt.Sprintf("entity-%02d", i))
		}
		gatewayTags := []string{"gateway-a", "gateway-b", "gateway-c"}
		classTags := []string{"class-a"}

		got := MergeTags(logr.Discard(), entityTags, gatewayTags, classTags)

		require.Len(t, got, MaxTags)
		assert.Equal(t, append(append([]string{}, entityTags...), "gateway-a", "gateway-b"), got)
		assert.NotContains(t, got, "gateway-c")
		assert.NotContains(t, got, "class-a")
	})
}

func TestInheritedTagsForRoute(t *testing.T) {
	tests := []struct {
		name     string
		objects  []runtime.Object
		route    client.Object
		expected []string
	}{
		{
			name: "Gateway tags precede GatewayClass tags",
			objects: []runtime.Object{
				tagsGateway("ns", "gw", "gwc", "gw-tag"),
				tagsGatewayClass("gwc", "class-tag"),
			},
			route:    tagsHTTPRoute("route", "gw"),
			expected: []string{"gw-tag", "class-tag"},
		},
		{
			name: "Gateway tags only",
			objects: []runtime.Object{
				tagsGateway("ns", "gw", "gwc", "gw-tag"),
				tagsGatewayClass("gwc", ""),
			},
			route:    tagsHTTPRoute("route", "gw"),
			expected: []string{"gw-tag"},
		},
		{
			name: "GatewayClass tags only",
			objects: []runtime.Object{
				tagsGateway("ns", "gw", "gwc", ""),
				tagsGatewayClass("gwc", "class-tag"),
			},
			route:    tagsHTTPRoute("route", "gw"),
			expected: []string{"class-tag"},
		},
		{
			name:     "missing Gateway contributes nothing",
			route:    tagsHTTPRoute("route", "ghost"),
			expected: nil,
		},
		{
			name: "missing GatewayClass leaves the Gateway tags intact",
			objects: []runtime.Object{
				tagsGateway("ns", "gw", "does-not-exist", "gw-tag"),
			},
			route:    tagsHTTPRoute("route", "gw"),
			expected: []string{"gw-tag"},
		},
		{
			name: "empty gatewayClassName is not looked up",
			objects: []runtime.Object{
				tagsGateway("ns", "gw", "", "gw-tag"),
			},
			route:    tagsHTTPRoute("route", "gw"),
			expected: []string{"gw-tag"},
		},
		{
			name:     "route without parentRefs contributes nothing",
			objects:  []runtime.Object{tagsGateway("ns", "gw", "gwc", "gw-tag")},
			route:    tagsHTTPRoute("route"),
			expected: nil,
		},
		{
			name: "several parentRefs are unioned in declaration order",
			objects: []runtime.Object{
				tagsGateway("ns", "gw-a", "gwc-a", "gw-a-tag"),
				tagsGateway("ns", "gw-b", "gwc-b", "gw-b-tag"),
				tagsGatewayClass("gwc-a", "class-a-tag"),
				tagsGatewayClass("gwc-b", "class-b-tag"),
			},
			route: tagsHTTPRoute("route", "gw-a", "gw-b"),
			// Every Gateway tag precedes every GatewayClass tag so the documented drop order
			// survives a route with more than one parent.
			expected: []string{"gw-a-tag", "gw-b-tag", "class-a-tag", "class-b-tag"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(tc.objects...).Build()
			pair := inheritedTagsForRoute(context.Background(), logr.Discard(), cl, tc.route)
			assert.Equal(t, tc.expected, slices.Concat(pair.gateway, pair.gatewayClass))
		})
	}
}

func TestInheritedTagsForRoute_ParentRefResolution(t *testing.T) {
	otherNamespace := gwtypes.Namespace("other-ns")
	emptyNamespace := gwtypes.Namespace("")
	serviceKind := gwtypes.Kind("Service")
	otherGroup := gwtypes.Group("example.com")

	tests := []struct {
		name      string
		objects   []runtime.Object
		parentRef gwtypes.ParentReference
		expected  []string
	}{
		{
			name:      "nil namespace defaults to the route's namespace",
			objects:   []runtime.Object{tagsGateway("ns", "gw", "", "gw-tag")},
			parentRef: gwtypes.ParentReference{Name: "gw"},
			expected:  []string{"gw-tag"},
		},
		{
			name:      "empty namespace defaults to the route's namespace",
			objects:   []runtime.Object{tagsGateway("ns", "gw", "", "gw-tag")},
			parentRef: gwtypes.ParentReference{Name: "gw", Namespace: &emptyNamespace},
			expected:  []string{"gw-tag"},
		},
		{
			name:      "explicit namespace is honoured",
			objects:   []runtime.Object{tagsGateway("other-ns", "gw", "", "gw-tag")},
			parentRef: gwtypes.ParentReference{Name: "gw", Namespace: &otherNamespace},
			expected:  []string{"gw-tag"},
		},
		{
			name:      "non-Gateway kind is ignored",
			objects:   []runtime.Object{tagsGateway("ns", "gw", "", "gw-tag")},
			parentRef: gwtypes.ParentReference{Name: "gw", Kind: &serviceKind},
			expected:  nil,
		},
		{
			name:      "non Gateway-API group is ignored",
			objects:   []runtime.Object{tagsGateway("ns", "gw", "", "gw-tag")},
			parentRef: gwtypes.ParentReference{Name: "gw", Group: &otherGroup},
			expected:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := &gwtypes.HTTPRoute{Name: "route", Namespace: "ns"}
			route.Spec.ParentRefs = []gwtypes.ParentReference{tc.parentRef}
			route.Status.Parents = []gwtypes.RouteParentStatus{{
				ParentRef:      tc.parentRef,
				ControllerName: gwtypes.GatewayController(vars.ControllerName()),
			}}

			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(tc.objects...).Build()
			pair := inheritedTagsForRoute(context.Background(), logr.Discard(), cl, route)
			assert.Equal(t, tc.expected, slices.Concat(pair.gateway, pair.gatewayClass))
		})
	}
}

// TestInheritedTagsForRoute_SchemeWithoutGatewayAPI pins the contract that a client which cannot
// even represent a Gateway yields no tags instead of erroring or panicking. Several factory tests
// build such clients.
func TestInheritedTagsForRoute_SchemeWithoutGatewayAPI(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	pair := inheritedTagsForRoute(context.Background(), logr.Discard(), cl, tagsHTTPRoute("route", "gw"))
	assert.Nil(t, slices.Concat(pair.gateway, pair.gatewayClass))
}

// TestInheritedTagsForRoute_OnlyParentsThisControllerReconciles is the regression guard for the
// whole filter: a route can be attached to Gateways owned by another implementation, and those
// produce none of the Kong entities being tagged.
func TestInheritedTagsForRoute_OnlyParentsThisControllerReconciles(t *testing.T) {
	objects := []runtime.Object{
		tagsGateway("ns", "ours", "our-class", "our-tag"),
		tagsGateway("ns", "theirs", "their-class", "their-tag"),
		tagsGatewayClass("our-class", "our-class-tag"),
		tagsGatewayClass("their-class", "their-class-tag"),
	}

	parentRef := func(name string) gwtypes.ParentReference {
		return gwtypes.ParentReference{Name: gwtypes.ObjectName(name)}
	}

	tests := []struct {
		name     string
		statuses []gwtypes.RouteParentStatus
		expected []string
	}{
		{
			name: "a parent owned by another controller contributes nothing",
			statuses: []gwtypes.RouteParentStatus{
				{ParentRef: parentRef("ours"), ControllerName: gwtypes.GatewayController(vars.ControllerName())},
				{ParentRef: parentRef("theirs"), ControllerName: "example.com/some-other-controller"},
			},
			expected: []string{"our-tag", "our-class-tag"},
		},
		{
			name: "a parent with no status entry at all contributes nothing",
			statuses: []gwtypes.RouteParentStatus{
				{ParentRef: parentRef("ours"), ControllerName: gwtypes.GatewayController(vars.ControllerName())},
			},
			expected: []string{"our-tag", "our-class-tag"},
		},
		{
			name:     "a route with no accepted parent inherits nothing",
			statuses: nil,
			expected: nil,
		},
		{
			name: "both parents accepted by us are unioned",
			statuses: []gwtypes.RouteParentStatus{
				{ParentRef: parentRef("ours"), ControllerName: gwtypes.GatewayController(vars.ControllerName())},
				{ParentRef: parentRef("theirs"), ControllerName: gwtypes.GatewayController(vars.ControllerName())},
			},
			// Every Gateway tag still precedes every GatewayClass tag.
			expected: []string{"our-tag", "their-tag", "our-class-tag", "their-class-tag"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := &gwtypes.HTTPRoute{Name: "route", Namespace: "ns"}
			route.Spec.ParentRefs = []gwtypes.ParentReference{parentRef("ours"), parentRef("theirs")}
			route.Status.Parents = tc.statuses

			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()
			pair := inheritedTagsForRoute(context.Background(), logr.Discard(), cl, route)
			assert.Equal(t, tc.expected, slices.Concat(pair.gateway, pair.gatewayClass))
		})
	}
}

func TestTagsForGatewayClassOf(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
		WithRuntimeObjects(tagsGatewayClass("gwc", "class-tag")).Build()
	ctx := context.Background()

	assert.Nil(t, TagsForGatewayClassOf(ctx, logr.Discard(), cl, nil), "a nil Gateway has no class")
	assert.Nil(t, TagsForGatewayClassOf(ctx, logr.Discard(), cl, tagsGateway("ns", "gw", "", "")),
		"an empty gatewayClassName is not looked up")
	assert.Nil(t, TagsForGatewayClassOf(ctx, logr.Discard(), cl, tagsGateway("ns", "gw", "ghost", "")),
		"a missing GatewayClass contributes nothing")
	assert.Equal(t, []string{"class-tag"},
		TagsForGatewayClassOf(ctx, logr.Discard(), cl, tagsGateway("ns", "gw", "gwc", "")))
}

// kongServiceAttachedTo builds a KongService carrying the hybrid-routes annotation for the given
// HTTPRoutes, mirroring what translator.VerifyAndUpdate writes.
func kongServiceAttachedTo(routes ...*gwtypes.HTTPRoute) *configurationv1alpha1.KongService {
	keys := make([]string, 0, len(routes))
	for _, route := range routes {
		keys = append(keys, client.ObjectKeyFromObject(route).String())
	}
	return &configurationv1alpha1.KongService{
		Name:      "shared-service",
		Namespace: "ns",
		Annotations: map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: strings.Join(keys, ","),
		},
	}
}

func TestInheritedTagsForKongObject(t *testing.T) {
	routeA := tagsHTTPRoute("route-a", "gw-a")
	routeB := tagsHTTPRoute("route-b", "gw-b")
	gateways := []runtime.Object{
		tagsGateway("ns", "gw-a", "gwc-a", "team-a"),
		tagsGateway("ns", "gw-b", "gwc-b", "team-b"),
		tagsGatewayClass("gwc-a", "class-a"),
		tagsGatewayClass("gwc-b", "class-b"),
	}

	t.Run("an object that does not exist yet only sees the reconciled route", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
			WithRuntimeObjects(append(append([]runtime.Object{}, gateways...), routeA, routeB)...).Build()

		got := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeA,
			&configurationv1alpha1.KongService{Name: "shared-service", Namespace: "ns"})
		assert.Equal(t, []string{"team-a", "class-a"}, got)
	})

	t.Run("a shared object yields the same tags whichever route is reconciled", func(t *testing.T) {
		objects := append(append([]runtime.Object{}, gateways...), routeA, routeB, kongServiceAttachedTo(routeA, routeB))
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()

		// The value must not depend on which route triggered the reconcile: two reconcilers
		// writing the same shared KongService under one field manager would otherwise overwrite
		// each other's tags forever.
		want := []string{"team-a", "team-b", "class-a", "class-b"}
		fromA := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA, routeB))
		fromB := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeB, kongServiceAttachedTo(routeA, routeB))
		assert.Equal(t, want, fromA)
		assert.Equal(t, fromA, fromB)
	})

	t.Run("the annotation order of the attached routes does not matter", func(t *testing.T) {
		objects := append(append([]runtime.Object{}, gateways...), routeA, routeB, kongServiceAttachedTo(routeB, routeA))
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()

		got := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeB, kongServiceAttachedTo(routeB, routeA))
		assert.Equal(t, []string{"team-a", "team-b", "class-a", "class-b"}, got)
	})

	t.Run("tags shrink when a route detaches", func(t *testing.T) {
		objects := append(append([]runtime.Object{}, gateways...), routeA, routeB, kongServiceAttachedTo(routeA))
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()

		got := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA))
		assert.Equal(t, []string{"team-a", "class-a"}, got)
	})

	t.Run("an attached route that no longer exists is skipped", func(t *testing.T) {
		objects := append(append([]runtime.Object{}, gateways...), routeA, kongServiceAttachedTo(routeA, routeB))
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()

		got := InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA, routeB))
		assert.Equal(t, []string{"team-a", "class-a"}, got)
	})
}

// TestInheritedTagsForKongObject_MemoizesRouteLookups pins that the attached-route lookups are
// memoized per reconcile: a shared entity is resolved once per entity, and translating many of
// them must not re-read every attached route each time.
func TestInheritedTagsForKongObject_MemoizesRouteLookups(t *testing.T) {
	routeA := tagsHTTPRoute("route-a", "gw-a")
	routeB := tagsHTTPRoute("route-b", "gw-b")
	objects := []runtime.Object{
		tagsGateway("ns", "gw-a", "gwc-a", "team-a"),
		tagsGateway("ns", "gw-b", "gwc-b", "team-b"),
		tagsGatewayClass("gwc-a", "class-a"),
		tagsGatewayClass("gwc-b", "class-b"),
		routeA, routeB, kongServiceAttachedTo(routeA, routeB),
	}

	var gets int
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				gets++
				return c.Get(ctx, key, obj, opts...)
			},
		}).Build()

	ctx := WithTagCache(context.Background())
	want := []string{"team-a", "team-b", "class-a", "class-b"}

	assert.Equal(t, want, InheritedTagsForKongObject(ctx, logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA, routeB)))
	afterFirst := gets

	// A second entity in the same reconcile re-reads only the entity itself.
	assert.Equal(t, want, InheritedTagsForKongObject(ctx, logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA, routeB)))
	assert.Equal(t, 1, gets-afterFirst,
		"only the Kong entity itself should be re-read; the routes, Gateways and GatewayClasses are memoized")
}

// TestInheritedTagsForKongObject_SkipsUnmanagedParents checks the filter still applies to the
// other routes attached to a shared entity, not just to the route being reconciled.
func TestInheritedTagsForKongObject_SkipsUnmanagedParents(t *testing.T) {
	routeA := tagsHTTPRoute("route-a", "gw-a")

	// routeB is attached to the same KongService but one of its parents belongs to another
	// controller, so only gw-b contributes.
	routeB := &gwtypes.HTTPRoute{Name: "route-b", Namespace: "ns"}
	routeB.Spec.ParentRefs = []gwtypes.ParentReference{{Name: "gw-b"}, {Name: "theirs"}}
	routeB.Status.Parents = []gwtypes.RouteParentStatus{
		{ParentRef: gwtypes.ParentReference{Name: "gw-b"}, ControllerName: gwtypes.GatewayController(vars.ControllerName())},
		{ParentRef: gwtypes.ParentReference{Name: "theirs"}, ControllerName: "example.com/some-other-controller"},
	}

	objects := []runtime.Object{
		tagsGateway("ns", "gw-a", "gwc-a", "team-a"),
		tagsGateway("ns", "gw-b", "gwc-b", "team-b"),
		tagsGateway("ns", "theirs", "their-class", "their-tag"),
		tagsGatewayClass("gwc-a", "class-a"),
		tagsGatewayClass("gwc-b", "class-b"),
		tagsGatewayClass("their-class", "their-class-tag"),
		routeA, routeB, kongServiceAttachedTo(routeA, routeB),
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(objects...).Build()

	want := []string{"team-a", "team-b", "class-a", "class-b"}
	assert.Equal(t, want,
		InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeA, kongServiceAttachedTo(routeA, routeB)))
	assert.Equal(t, want,
		InheritedTagsForKongObject(context.Background(), logr.Discard(), cl, routeB, kongServiceAttachedTo(routeA, routeB)),
		"the value must not depend on which route is reconciled")
}

func TestAppendTagsAnnotation(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		tags     []string
		expected string
	}{
		{
			name:     "no tags leaves the annotation untouched",
			existing: "plugin-tag",
			tags:     nil,
			expected: "plugin-tag",
		},
		{
			name:     "tags are appended after the existing ones",
			existing: "plugin-tag",
			tags:     []string{"gw-tag", "class-tag"},
			expected: "plugin-tag,gw-tag,class-tag",
		},
		{
			name:     "sets the annotation when it is absent",
			tags:     []string{"gw-tag"},
			expected: "gw-tag",
		},
		{
			name:     "duplicates of existing tags are dropped",
			existing: "shared,plugin-tag",
			tags:     []string{"shared", "gw-tag"},
			expected: "shared,plugin-tag,gw-tag",
		},
		{
			name:     "tags with reserved prefixes are not appended",
			existing: "plugin-tag",
			tags:     []string{"gw-tag", "k8s-uid:1234", "managed-by:someone-else"},
			expected: "plugin-tag,gw-tag",
		},
		{
			name:     "only reserved tags leave the annotation absent",
			tags:     []string{"k8s-name:other"},
			expected: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &configurationv1alpha1.KongService{Name: "obj", Namespace: "ns"}
			if tc.existing != "" {
				plugin.Annotations = map[string]string{pkgmetadata.AnnotationKeyTags: tc.existing}
			}

			AppendTagsAnnotation(logr.Discard(), plugin, tc.tags)
			assert.Equal(t, tc.expected, plugin.Annotations[pkgmetadata.AnnotationKeyTags])
		})
	}
}

// TestWithTagCache checks that the cache is transparent: resolving through a cached context must
// give the same answer as resolving without one, and it must be reused rather than re-created.
func TestWithTagCache(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRuntimeObjects(
		tagsGateway("ns", "gw", "gwc", "gw-tag"), tagsGatewayClass("gwc", "class-tag"),
	).Build()
	route := tagsHTTPRoute("route", "gw")

	plain := context.Background()
	cached := WithTagCache(plain)
	require.NotNil(t, tagCacheFrom(cached))
	require.Nil(t, tagCacheFrom(plain), "the original context must not be modified")
	require.Same(t, tagCacheFrom(cached), tagCacheFrom(WithTagCache(cached)), "an existing cache is reused")

	want := inheritedTagsForRoute(plain, logr.Discard(), cl, route)
	assert.Equal(t, want, inheritedTagsForRoute(cached, logr.Discard(), cl, route))
	// Second call comes from the cache.
	assert.Equal(t, want, inheritedTagsForRoute(cached, logr.Discard(), cl, route))
}
