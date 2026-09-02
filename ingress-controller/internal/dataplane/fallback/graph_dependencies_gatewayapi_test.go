package fallback_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

func TestResolveDependencies_HTTPRoute(t *testing.T) {
	testCases := []resolveDependenciesTestCase{
		{
			name: "no dependencies",
			object: &gatewayapi.HTTPRoute{
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
			object: &gatewayapi.HTTPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gatewayapi.HTTPRouteSpec{
					Rules: []gatewayapi.HTTPRouteRule{
						{
							BackendRefs: []gatewayapi.HTTPBackendRef{
								{
									Name: "1",
									Kind: new(gatewayapi.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gatewayapi.Kind("Service")),
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
			object: &gatewayapi.HTTPRoute{
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
			object: &gatewayapi.TLSRoute{
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
			object: &gatewayapi.TLSRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gatewayapi.TLSRouteSpec{
					Rules: []gatewayapi.TLSRouteRule{
						{
							BackendRefs: []gatewayapi.BackendRef{
								{
									Name: "1",
									Kind: new(gatewayapi.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gatewayapi.Kind("Service")),
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
			object: &gatewayapi.TLSRoute{
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
			object: &gatewayapi.TCPRoute{
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
			object: &gatewayapi.TCPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gatewayapi.TCPRouteSpec{
					Rules: []gatewayapi.TCPRouteRule{
						{
							BackendRefs: []gatewayapi.BackendRef{
								{
									Name: "1",
									Kind: new(gatewayapi.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gatewayapi.Kind("Service")),
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
			object: &gatewayapi.TCPRoute{
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
			object: &gatewayapi.UDPRoute{
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
			object: &gatewayapi.UDPRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gatewayapi.UDPRouteSpec{
					Rules: []gatewayapi.UDPRouteRule{
						{
							BackendRefs: []gatewayapi.BackendRef{
								{
									Name: "1",
									Kind: new(gatewayapi.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gatewayapi.Kind("Service")),
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
			object: &gatewayapi.UDPRoute{
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
			object: &gatewayapi.GRPCRoute{
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
			object: &gatewayapi.GRPCRoute{
				Name:      "test-route",
				Namespace: "test-namespace",
				Spec: gatewayapi.GRPCRouteSpec{
					Rules: []gatewayapi.GRPCRouteRule{
						{
							BackendRefs: []gatewayapi.GRPCBackendRef{
								{
									Name: "1",
									Kind: new(gatewayapi.Kind("Service")),
								},
								{
									Name: "2",
									Kind: new(gatewayapi.Kind("Service")),
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
			object: &gatewayapi.GRPCRoute{
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
