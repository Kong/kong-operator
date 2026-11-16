package translator

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	mgrconsts "github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func withTags[T interface{ SetAnnotations(map[string]string) }](obj T, tags string) T {
	obj.SetAnnotations(map[string]string{annotations.AnnotationPrefix + annotations.UserTagKey: tags})
	return obj
}

func gatewayParentRef(ns, name string) gatewayv1.ParentReference {
	pr := gatewayv1.ParentReference{Name: gatewayv1.ObjectName(name)}
	if ns != "" {
		pr.Namespace = new(gatewayv1.Namespace(ns))
	}
	return pr
}

func commonRouteSpec(parentRefs ...gatewayv1.ParentReference) gatewayv1.CommonRouteSpec {
	return gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}
}

func TestBuildInheritedTagIndex(t *testing.T) {
	ownedGWC := withTags(mkGatewayClass("owned", mgrconsts.GetControllerName()), "gwc-tag, shared")
	untaggedGWC := mkGatewayClass("untagged", mgrconsts.GetControllerName())
	otherGWC := withTags(mkGatewayClass("other", "example.com/other-controller"), "other-gwc-tag")

	gw1 := withTags(mkGateway("default", "gw-1", "owned"), "gw-1-tag,shared")
	gw2 := withTags(mkGateway("infra", "gw-2", "untagged"), "gw-2-tag")
	gwUntagged := mkGateway("default", "gw-untagged", "owned")
	gwOtherClass := withTags(mkGateway("default", "gw-other-class", "other"), "gw-other-class-tag")
	gwMissingClass := withTags(mkGateway("default", "gw-missing-class", "missing"), "gw-missing-class-tag")

	httpRoutes := []*gatewayapi.HTTPRoute{
		{
			Namespace: "default", Name: "single-parent",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		},
		{
			Namespace: "default", Name: "two-parents",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(
				gatewayParentRef("", "gw-1"),
				gatewayParentRef("infra", "gw-2"),
				// The same Gateway again (e.g. another listener) must not duplicate its tags.
				func() gatewayv1.ParentReference {
					pr := gatewayParentRef("default", "gw-1")
					pr.SectionName = new(gatewayv1.SectionName("https"))
					return pr
				}(),
			)},
		},
		{
			Namespace: "default", Name: "class-tags-only",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-untagged"))},
		},
		{
			Namespace: "default", Name: "gateway-of-other-controller-class",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-other-class"))},
		},
		{
			Namespace: "default", Name: "gateway-with-missing-class",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-missing-class"))},
		},
		{
			Namespace: "default", Name: "gateway-not-in-store",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "not-ours"))},
		},
		{
			// The parentRef defaults to the route's namespace, where there is no gw-1.
			Namespace: "other", Name: "gateway-in-other-namespace",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		},
		{
			Namespace: "default", Name: "non-gateway-parent",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(
				func() gatewayv1.ParentReference {
					pr := gatewayParentRef("", "gw-1")
					pr.Kind = new(gatewayv1.Kind("Service"))
					pr.Group = new(gatewayv1.Group(""))
					return pr
				}(),
				func() gatewayv1.ParentReference {
					pr := gatewayParentRef("", "gw-1")
					pr.Group = new(gatewayv1.Group("example.com"))
					return pr
				}(),
			)},
		},
		{
			Namespace: "default", Name: "explicit-gateway-group-and-kind",
			Spec: gatewayapi.HTTPRouteSpec{CommonRouteSpec: commonRouteSpec(
				func() gatewayv1.ParentReference {
					pr := gatewayParentRef("", "gw-1")
					pr.Kind = new(KindGateway)
					pr.Group = new(gatewayapi.V1Group)
					return pr
				}(),
			)},
		},
	}

	s, err := store.NewFakeStore(store.FakeObjects{
		GatewayClasses: []*gatewayapi.GatewayClass{ownedGWC, untaggedGWC, otherGWC},
		Gateways:       []*gatewayapi.Gateway{gw1, gw2, gwUntagged, gwOtherClass, gwMissingClass},
		HTTPRoutes:     httpRoutes,
		GRPCRoutes: []*gatewayapi.GRPCRoute{{
			Namespace: "default", Name: "grpc",
			Spec: gatewayapi.GRPCRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		}},
		TCPRoutes: []*gatewayapi.TCPRoute{{
			Namespace: "default", Name: "tcp",
			Spec: gatewayapi.TCPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		}},
		UDPRoutes: []*gatewayapi.UDPRoute{{
			Namespace: "default", Name: "udp",
			Spec: gatewayapi.UDPRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		}},
		TLSRoutes: []*gatewayapi.TLSRoute{{
			Namespace: "default", Name: "tls",
			Spec: gatewayapi.TLSRouteSpec{CommonRouteSpec: commonRouteSpec(gatewayParentRef("", "gw-1"))},
		}},
	})
	require.NoError(t, err)
	tr := mustNewTranslator(t, s)

	index := tr.buildInheritedTagIndex(newInheritedTagResolver(logr.Discard(), s))

	gw1Tags := util.InheritedTags{Gateway: []string{"gw-1-tag", "shared"}, GatewayClass: []string{"gwc-tag", "shared"}}
	assert.Equal(t, inheritedTagIndex{
		"HTTPRoute/default/single-parent": gw1Tags,
		"HTTPRoute/default/two-parents": {
			Gateway:      []string{"gw-1-tag", "shared", "gw-2-tag"},
			GatewayClass: []string{"gwc-tag", "shared"},
		},
		"HTTPRoute/default/class-tags-only":                   {GatewayClass: []string{"gwc-tag", "shared"}},
		"HTTPRoute/default/gateway-of-other-controller-class": {Gateway: []string{"gw-other-class-tag"}},
		"HTTPRoute/default/gateway-with-missing-class":        {Gateway: []string{"gw-missing-class-tag"}},
		"HTTPRoute/default/explicit-gateway-group-and-kind":   gw1Tags,
		"GRPCRoute/default/grpc":                              gw1Tags,
		"TCPRoute/default/tcp":                                gw1Tags,
		"UDPRoute/default/udp":                                gw1Tags,
		"TLSRoute/default/tls":                                gw1Tags,
	}, index)

	t.Run("all returns gateway tags, then gateway class tags, deduplicated", func(t *testing.T) {
		assert.Equal(t, []string{"gw-1-tag", "shared", "gw-2-tag", "gwc-tag"}, index["HTTPRoute/default/two-parents"].All())
	})
}

func TestApplyInheritedTags(t *testing.T) {
	httpRouteGVK := schema.GroupVersionKind{Group: gatewayapi.GroupVersion.Group, Version: "v1", Kind: "HTTPRoute"}
	ingressGVK := schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}

	index := inheritedTagIndex{
		"HTTPRoute/default/route-a": {Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}},
		"HTTPRoute/default/route-b": {Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}},
	}

	newRoute := func(name string, gvk schema.GroupVersionKind, objName string) kongstate.Route {
		tags := kong.StringSlice("k8s-name:"+objName, "own")
		return kongstate.Route{
			Name: new(name), Tags: tags,
			Ingress: util.K8sObjectInfo{
				Name: objName, Namespace: "default", GroupVersionKind: gvk,
			},
			// Plugins generated from filters share the route's tags slice.
			Plugins: []kong.Plugin{{Name: new("request-transformer"), Tags: tags}},
		}
	}

	t.Run("no inherited tags is a no-op", func(t *testing.T) {
		ir := newIngressRules()
		ir.ServiceNameToServices["svc"] = kongstate.Service{
			Name:   new("svc"),
			Routes: []kongstate.Route{newRoute("r", httpRouteGVK, "route-a")},
		}
		tr := mustNewTranslator(t, mustNewEmptyFakeStore(t))
		tr.applyInheritedTags(&ir, inheritedTagIndex{})
		svc := ir.ServiceNameToServices["svc"]
		assert.True(t, svc.InheritedTags.IsEmpty())
		assert.True(t, svc.Routes[0].InheritedTags.IsEmpty())
		assert.Equal(t, kong.StringSlice("k8s-name:route-a", "own"), svc.Routes[0].Tags)
	})

	t.Run("routes, their plugins and service plugins inherit tags", func(t *testing.T) {
		ir := newIngressRules()
		ir.ServiceNameToServices["combined"] = kongstate.Service{
			Name:   new("combined"),
			Parent: &gatewayapi.HTTPRoute{Namespace: "default", Name: "route-b"},
			// route-b comes first, but the Service's inherited tags are ordered by route key.
			Routes: []kongstate.Route{
				newRoute("rb", httpRouteGVK, "route-b"),
				newRoute("ra", httpRouteGVK, "route-a"),
			},
			Plugins: []kong.Plugin{{Name: new("request-termination")}},
		}
		ir.ServiceNameToServices["ingress"] = kongstate.Service{
			Name:   new("ingress"),
			Routes: []kongstate.Route{newRoute("ri", ingressGVK, "route-a")},
		}
		tr := mustNewTranslator(t, mustNewEmptyFakeStore(t))

		tr.applyInheritedTags(&ir, index)

		combined := ir.ServiceNameToServices["combined"]
		assert.Equal(t, util.InheritedTags{Gateway: []string{"gw-a", "gw-b"}, GatewayClass: []string{"gwc"}}, combined.InheritedTags)
		assert.Equal(t, kong.StringSlice("gw-a", "gw-b", "gwc"), combined.Plugins[0].Tags)

		rb := combined.Routes[0]
		assert.Equal(t, util.InheritedTags{Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}}, rb.InheritedTags)
		assert.Equal(t, kong.StringSlice("k8s-name:route-b", "own", "gw-b", "gwc"), rb.Tags)
		assert.Equal(t, kong.StringSlice("k8s-name:route-b", "own", "gw-b", "gwc"), rb.Plugins[0].Tags)
		// The route's and its plugins' tags must not alias each other anymore.
		require.NotSame(t, &rb.Tags[0], &rb.Plugins[0].Tags[0])

		ra := combined.Routes[1]
		assert.Equal(t, util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}}, ra.InheritedTags)
		assert.Equal(t, kong.StringSlice("k8s-name:route-a", "own", "gw-a", "gwc"), ra.Tags)

		// Entities translated from an Ingress with the same name do not inherit anything.
		ingress := ir.ServiceNameToServices["ingress"]
		assert.True(t, ingress.InheritedTags.IsEmpty())
		assert.True(t, ingress.Routes[0].InheritedTags.IsEmpty())
		assert.Equal(t, kong.StringSlice("k8s-name:route-a", "own"), ingress.Routes[0].Tags)
	})

	t.Run("service without routes inherits from its parent route", func(t *testing.T) {
		ir := newIngressRules()
		ir.ServiceNameToServices["svc"] = kongstate.Service{
			Name:   new("svc"),
			Parent: &gatewayapi.HTTPRoute{Namespace: "default", Name: "route-a"},
		}
		tr := mustNewTranslator(t, mustNewEmptyFakeStore(t))
		tr.applyInheritedTags(&ir, index)
		assert.Equal(t, util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}}, ir.ServiceNameToServices["svc"].InheritedTags)
	})
}

func mustNewEmptyFakeStore(t *testing.T) store.Storer {
	t.Helper()
	s, err := store.NewFakeStore(store.FakeObjects{})
	require.NoError(t, err)
	return s
}

func TestGenerateKongServiceTagsWithInheritedTags(t *testing.T) {
	route := withTags(&gatewayapi.HTTPRoute{
		APIVersion: gatewayapi.GroupVersion.String(), Kind: "HTTPRoute",
		Namespace: "default", Name: "route",
	}, "route-tag,shared")
	svc1 := withTags(&corev1.Service{
		APIVersion: "v1", Kind: "Service",
		Namespace: "default", Name: "svc-1",
	}, "svc-tag,shared")
	svc2 := &corev1.Service{Namespace: "default", Name: "svc-2"}

	ir := newIngressRules()
	ir.ServiceNameToParent["multi"] = route
	inherited := util.InheritedTags{Gateway: []string{"gw", "shared"}, GatewayClass: []string{"gwc"}}

	testCases := []struct {
		name        string
		k8sServices []*corev1.Service
		serviceName string
		expected    []*string
	}{
		{
			name:        "single backend: K8s Service tags, then inherited tags",
			k8sServices: []*corev1.Service{svc1},
			serviceName: "single",
			expected: kong.StringSlice(
				"k8s-name:svc-1", "k8s-namespace:default", "k8s-kind:Service", "k8s-version:v1",
				"svc-tag", "shared", "gw", "gwc",
			),
		},
		{
			name:        "multiple backends: parent route tags, then inherited tags",
			k8sServices: []*corev1.Service{svc1, svc2},
			serviceName: "multi",
			expected: kong.StringSlice(
				"k8s-name:route", "k8s-namespace:default", "k8s-kind:HTTPRoute",
				"k8s-group:gateway.networking.k8s.io", "k8s-version:v1",
				"route-tag", "shared", "gw", "gwc",
			),
		},
		{
			name:        "no backends: placeholder tags, then inherited tags",
			serviceName: "none",
			expected: kong.StringSlice(
				"k8s-name:UNKNOWN", "k8s-namespace:UNKNOWN", "k8s-kind:Service",
				"k8s-uid:00000000-0000-0000-0000-000000000000", "k8s-group:core", "k8s-version:v1",
				"gw", "shared", "gwc",
			),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service := kongstate.Service{
				Name:          new(tc.serviceName),
				InheritedTags: inherited,
			}
			assert.Equal(t, tc.expected, ir.generateKongServiceTags(tc.k8sServices, service, logr.Discard()))
		})
	}
}

func TestClientCertificateInheritsInheritedTags(t *testing.T) {
	crt, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithKeyType(certificate.ECDSA), certificate.WithCommonName("client.example.com"),
	)
	clientSecret := &corev1.Secret{
		APIVersion: "v1", Kind: "Secret",
		Namespace: "default", Name: "client-cert", UID: "client-cert-uid",
		Annotations: map[string]string{annotations.AnnotationPrefix + annotations.UserTagKey: "secret-tag"},
		Type:        corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       crt,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	s, err := store.NewFakeStore(store.FakeObjects{Secrets: []*corev1.Secret{clientSecret}})
	require.NoError(t, err)
	tr := mustNewTranslator(t, s)

	k8sService := &corev1.Service{
		APIVersion: "v1", Kind: "Service",
		Namespace: "default", Name: "backend",
		Annotations: map[string]string{
			annotations.AnnotationPrefix + annotations.ClientCertKey: "client-cert",
			annotations.AnnotationPrefix + annotations.ProtocolKey:   "https",
		},
	}

	newKongService := func(name string, inherited util.InheritedTags) kongstate.Service {
		return kongstate.Service{
			Name: new(name), Protocol: new("https"),
			InheritedTags: inherited,
		}
	}
	svcA := newKongService("svc-a", util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}})
	svcB := newKongService("svc-b", util.InheritedTags{Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}})
	plain := newKongService("svc-plain", util.InheritedTags{})

	failuresCollector := failures.NewResourceFailuresCollector(logr.Discard())
	ir := newIngressRules()
	// svc-b first to show the order does not depend on the insertion order.
	ir.handleServiceClientCertificates(s, k8sService.DeepCopy(), &svcB, failuresCollector)
	ir.handleServiceClientCertificates(s, k8sService.DeepCopy(), &svcA, failuresCollector)
	// A Service with several backends referencing the same Secret reaches this point once per backend.
	ir.handleServiceClientCertificates(s, k8sService.DeepCopy(), &svcA, failuresCollector)
	// A Service without inherited tags is not recorded.
	ir.handleServiceClientCertificates(s, k8sService.DeepCopy(), &plain, failuresCollector)
	require.Empty(t, failuresCollector.PopResourceFailures())

	assert.Equal(t, map[string][]clientCertServiceTags{
		"default/client-cert": {
			{ServiceName: "svc-b", InheritedTags: util.InheritedTags{Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}}},
			{ServiceName: "svc-a", InheritedTags: util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}}},
		},
	}, ir.ClientCertSecretToServices)

	wrappers := tr.getCerts(ir.SecretNameToSNIs, ir.ClientCertSecretToServices)
	require.Len(t, wrappers, 1)
	merged, _ := mergeCerts(logr.Discard(), wrappers)
	require.Len(t, merged, 1)
	assert.Equal(t, kong.StringSlice(
		"k8s-name:client-cert", "k8s-namespace:default", "k8s-kind:Secret", "k8s-uid:client-cert-uid", "k8s-version:v1",
		"secret-tag",
		// The Services' inherited tags follow the Secret's own tags: all Gateways' tags (in
		// service-name order), then the GatewayClasses' tags, deduplicated.
		"gw-a", "gw-b", "gwc",
	), merged[0].Tags)
}
