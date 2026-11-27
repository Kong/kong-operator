package fallback_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestResolveDependencies_HTTPRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{},
		},
		{
			name: "HTTPRoute -> Service",
			object: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								{
									Name: "1",
									Kind: new(gwtypes.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gwtypes.Kind("Service")),
								},
							},
						},
					},
				},
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
			),
			expected: []client.Object{
				testService(t, "1"),
				testService(t, "2"),
			},
		},
		{
			name: "HTTPRoute -> KongPlugin, KongClusterPlugin",
			object: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.PluginsKey: "1,2,cluster-1,cluster-2",
				},
			},
			cache: cacheStoresFromObjs(t,
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			},
		},
	}

	for _, tc := range testCases {
		runResolveDependenciesTest(t, tc)
	}
}

func TestResolveDependencies_TLSRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gwtypes.TLSRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{},
		},
		{
			name: "TLSRoute -> Service",
			object: &gwtypes.TLSRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gwtypes.TLSRouteSpec{
					Rules: []gwtypes.TLSRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									Name: "1",
									Kind: new(gwtypes.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gwtypes.Kind("Service")),
								},
							},
						},
					},
				},
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
			),
			expected: []client.Object{
				testService(t, "1"),
				testService(t, "2"),
			},
		},
		{
			name: "TLSRoute -> KongPlugin, KongClusterPlugin",
			object: &gwtypes.TLSRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.PluginsKey: "1,2,cluster-1,cluster-2",
				},
			},
			cache: cacheStoresFromObjs(t,
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			},
		},
	}

	for _, tc := range testCases {
		runResolveDependenciesTest(t, tc)
	}
}

func TestResolveDependencies_TCPRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gwtypes.TCPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{},
		},
		{
			name: "TCPRoute -> Service",
			object: &gwtypes.TCPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gwtypes.TCPRouteSpec{
					Rules: []gwtypes.TCPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									Name: "1",
									Kind: new(gwtypes.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gwtypes.Kind("Service")),
								},
							},
						},
					},
				},
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
			),
			expected: []client.Object{
				testService(t, "1"),
				testService(t, "2"),
			},
		},
		{
			name: "TCPRoute -> KongPlugin, KongClusterPlugin",
			object: &gwtypes.TCPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.PluginsKey: "1,2,cluster-1,cluster-2",
				},
			},
			cache: cacheStoresFromObjs(t,
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			},
		},
	}

	for _, tc := range testCases {
		runResolveDependenciesTest(t, tc)
	}
}

func TestResolveDependencies_UDPRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gwtypes.UDPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{},
		},
		{
			name: "UDPRoute -> Service",
			object: &gwtypes.UDPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gwtypes.UDPRouteSpec{
					Rules: []gwtypes.UDPRouteRule{
						{
							BackendRefs: []gwtypes.BackendRef{
								{
									Name: "1",
									Kind: new(gwtypes.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gwtypes.Kind("Service")),
								},
							},
						},
					},
				},
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
			),
			expected: []client.Object{
				testService(t, "1"),
				testService(t, "2"),
			},
		},
		{
			name: "UDPRoute -> KongPlugin, KongClusterPlugin",
			object: &gwtypes.UDPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.PluginsKey: "1,2,cluster-1,cluster-2",
				},
			},
			cache: cacheStoresFromObjs(t,
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			},
		},
	}

	for _, tc := range testCases {
		runResolveDependenciesTest(t, tc)
	}
}

func TestResolveDependencies_GRPCRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gwtypes.GRPCRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{},
		},
		{
			name: "GRPCRoute -> Service",
			object: &gwtypes.GRPCRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gwtypes.GRPCRouteSpec{
					Rules: []gwtypes.GRPCRouteRule{
						{
							BackendRefs: []gwtypes.GRPCBackendRef{
								{
									Name: "1",
									Kind: new(gwtypes.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gwtypes.Kind("Service")),
								},
							},
						},
					},
				},
			},
			cache: cacheStoresFromObjs(t,
				testService(t, "1"),
				testService(t, "2"),
			),
			expected: []client.Object{
				testService(t, "1"),
				testService(t, "2"),
			},
		},
		{
			name: "GRPCRoute -> KongPlugin, KongClusterPlugin",
			object: &gwtypes.GRPCRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.PluginsKey: "1,2,cluster-1,cluster-2",
				},
			},
			cache: cacheStoresFromObjs(t,
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			),
			expected: []client.Object{
				testKongPlugin(t, "1"),
				testKongPlugin(t, "2"),
				testKongClusterPlugin(t, "cluster-1"),
				testKongClusterPlugin(t, "cluster-2"),
			},
		},
	}

	for _, tc := range testCases {
		runResolveDependenciesTest(t, tc)
	}
}
