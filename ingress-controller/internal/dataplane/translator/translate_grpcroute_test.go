package translator

import (
	"strconv"
	"testing"

	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator/subtranslator"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestIngressRulesFromGRPCRoutesUsingExpressionRoutes(t *testing.T) {
	grpcRouteTypeMeta := gwtypes.GRPCRouteTypeMeta

	testCases := []struct {
		name                 string
		grpcRoutes           []*gwtypes.GRPCRoute
		expectedKongServices []kongstate.Service
		services             []*corev1.Service
		// service name -> routes
		expectedKongRoutes map[string][]kongstate.Route
	}{
		{
			name: "single GRPCRoute with multiple hostnames and multiple rules",
			grpcRoutes: []*gwtypes.GRPCRoute{
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-1",
					Spec: gwtypes.GRPCRouteSpec{
						Hostnames: []gwtypes.Hostname{
							"foo.com",
							"*.bar.com",
						},
						Rules: []gwtypes.GRPCRouteRule{
							{
								Matches: []gwtypes.GRPCRouteMatch{
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v1"),
											Method:  new("foo"),
										},
									},
								},
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service1").WithPort(80).Build(),
									},
								},
							},
							{
								Matches: []gwtypes.GRPCRouteMatch{
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v1"),
											Method:  new("foobar"),
										},
									},
								},
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service2").WithPort(80).Build(),
									},
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					Namespace: "default",
					Name:      "service1",
				},
				{
					Namespace: "default",
					Name:      "service2",
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Name: new("grpcroute.default.grpcroute-1.foo.com.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Name: new("grpcroute.default.grpcroute-1.foo.com.1"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service2").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Name: new("grpcroute.default.grpcroute-1._.bar.com.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Name: new("grpcroute.default.grpcroute-1._.bar.com.1"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service2").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"grpcroute.default.grpcroute-1.foo.com.0": {
					{
						Name:       new("grpcroute.default.grpcroute-1.foo.com.0.0"),
						Expression: new(`(http.path == "/v1/foo") && (http.host == "foo.com")`),
					},
				},
				"grpcroute.default.grpcroute-1.foo.com.1": {
					{
						Name:       new("grpcroute.default.grpcroute-1.foo.com.1.0"),
						Expression: new(`(http.path == "/v1/foobar") && (http.host == "foo.com")`),
					},
				},
				"grpcroute.default.grpcroute-1._.bar.com.0": {
					{
						Name:       new("grpcroute.default.grpcroute-1._.bar.com.0.0"),
						Expression: new(`(http.path == "/v1/foo") && (http.host =^ ".bar.com")`),
					},
				},
				"grpcroute.default.grpcroute-1._.bar.com.1": {
					{
						Name:       new("grpcroute.default.grpcroute-1._.bar.com.1.0"),
						Expression: new(`(http.path == "/v1/foobar") && (http.host =^ ".bar.com")`),
					},
				},
			},
		},
		{
			name: "multiple GRPCRoutes with multiple matches",
			grpcRoutes: []*gwtypes.GRPCRoute{
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-1",
					Spec: gwtypes.GRPCRouteSpec{
						Hostnames: []gwtypes.Hostname{
							"foo.com",
						},
						Rules: []gwtypes.GRPCRouteRule{
							{
								Matches: []gwtypes.GRPCRouteMatch{
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v1"),
											Method:  new("foo"),
										},
									},
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v1"),
											Method:  new("foobar"),
										},
									},
								},
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service1").WithPort(80).Build(),
									},
								},
							},
						},
					},
				},
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-2",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Matches: []gwtypes.GRPCRouteMatch{
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v2"),
											Method:  new("foo"),
										},
									},
								},
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service2").WithPort(80).Build(),
									},
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					Namespace: "default",
					Name:      "service1",
				},
				{
					Namespace: "default",
					Name:      "service2",
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Name: new("grpcroute.default.grpcroute-1.foo.com.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Name: new("grpcroute.default.grpcroute-2._.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service2").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"grpcroute.default.grpcroute-1.foo.com.0": {
					{
						Name:       new("grpcroute.default.grpcroute-1.foo.com.0.0"),
						Expression: new(`(http.path == "/v1/foo") && (http.host == "foo.com")`),
					},
					{
						Name:       new("grpcroute.default.grpcroute-1.foo.com.0.1"),
						Expression: new(`(http.path == "/v1/foobar") && (http.host == "foo.com")`),
					},
				},
				"grpcroute.default.grpcroute-2._.0": {
					{
						Name:       new("grpcroute.default.grpcroute-2._.0.0"),
						Expression: new(`http.path == "/v2/foo"`),
					},
				},
			},
		},
		{
			name: "multiple GRPCRoutes with translation error",
			grpcRoutes: []*gwtypes.GRPCRoute{
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-1",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								Matches: []gwtypes.GRPCRouteMatch{
									{
										Method: &gwtypes.GRPCMethodMatch{
											Type:    new(gwtypes.GRPCMethodMatchExact),
											Service: new("v1"),
											Method:  new("foo"),
										},
									},
								},
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service2").WithPort(80).Build(),
									},
								},
							},
						},
					},
				},
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-no-rules",
					Spec:      gwtypes.GRPCRouteSpec{},
				},
				{
					TypeMeta:  grpcRouteTypeMeta,
					Namespace: "default",
					Name:      "grpcroute-no-hostnames-no-matches",
					Spec: gwtypes.GRPCRouteSpec{
						Rules: []gwtypes.GRPCRouteRule{
							{
								BackendRefs: []gwtypes.GRPCBackendRef{
									{
										BackendRef: builder.NewBackendRef("service0").WithPort(80).Build(),
									},
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					Namespace: "default",
					Name:      "service0",
				},
				{
					Namespace: "default",
					Name:      "service2",
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Name: new("grpcroute.default.grpcroute-1._.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service2").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Name: new("grpcroute.default.grpcroute-no-hostnames-no-matches._.0"),
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service0").WithPortNumber(80).MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"grpcroute.default.grpcroute-1._.0": {
					{
						Name:       new("grpcroute.default.grpcroute-1._.0.0"),
						Expression: new(`http.path == "/v1/foo"`),
					},
				},
				"grpcroute.default.grpcroute-no-hostnames-no-matches._.0": {
					{
						Name:       new("grpcroute.default.grpcroute-no-hostnames-no-matches._.0.0"),
						Expression: kong.String(subtranslator.CatchAllHTTPExpression),
					},
				},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i)+"-"+tc.name, func(t *testing.T) {
			failureCollector := failures.NewResourceFailuresCollector(zapr.NewLogger(zap.NewNop()))

			fakestore, err := store.NewFakeStore(store.FakeObjects{
				GRPCRoutes: tc.grpcRoutes,
				Services:   tc.services,
			})
			require.NoError(t, err)
			translator := mustNewTranslator(t, fakestore)
			translator.featureFlags.ExpressionRoutes = true
			translator.failuresCollector = failureCollector

			result := newIngressRules()
			translator.ingressRulesFromGRPCRoutesUsingExpressionRoutes(tc.grpcRoutes, &result)
			// check services
			require.Len(t, result.ServiceNameToServices, len(tc.expectedKongServices),
				"should have expected number of services")
			for _, expectedKongService := range tc.expectedKongServices {
				kongService, ok := result.ServiceNameToServices[*expectedKongService.Name]
				require.Truef(t, ok, "should find service %s", expectedKongService.Name)
				require.Equal(t, expectedKongService.Backends, kongService.Backends)
				// check routes
				expectedKongRoutes := tc.expectedKongRoutes[*kongService.Name]
				require.Len(t, kongService.Routes, len(expectedKongRoutes))

				kongRouteNameToRoute := lo.SliceToMap(kongService.Routes, func(r kongstate.Route) (string, kongstate.Route) {
					return *r.Name, r
				})
				for _, expectedRoute := range expectedKongRoutes {
					routeName := expectedRoute.Name
					r, ok := kongRouteNameToRoute[*routeName]
					require.Truef(t, ok, "should find route %s", *routeName)
					require.Equal(t, *expectedRoute.Expression, *r.Expression)
				}
			}
			// Check translation failures.
			translationFailures := failureCollector.PopResourceFailures()
			require.Empty(t, translationFailures)
		})
	}
}

func TestGetProtocolsForKongRoute(t *testing.T) {
	testCases := []struct {
		name              string
		parentRefs        []gwtypes.ParentReference
		gateways          []*gwtypes.Gateway
		expectedProtocols []*string
	}{
		{
			name: "http listener returns grpc",
			parentRefs: []gwtypes.ParentReference{
				{
					Name: gwtypes.ObjectName("gw-http"),
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					Namespace: "default",
					Name:      "gw-http",
					Spec: gwtypes.GatewaySpec{
						Listeners: []gwtypes.Listener{
							{
								Name:     "http",
								Protocol: gwtypes.HTTPProtocolType,
								Port:     80,
							},
						},
					},
				},
			},
			expectedProtocols: kong.StringSlice("grpc"),
		},
		{
			name: "https listener returns grpcs",
			parentRefs: []gwtypes.ParentReference{
				{
					Name: gwtypes.ObjectName("gw-https"),
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					Namespace: "default", Name: "gw-https",
					Spec: gwtypes.GatewaySpec{
						Listeners: []gwtypes.Listener{
							{
								Name:     "https",
								Protocol: gwtypes.HTTPSProtocolType,
								Port:     443,
							},
						},
					},
				},
			},
			expectedProtocols: kong.StringSlice("grpcs"),
		},
		{
			name: "both listeners return grpc and grpcs",
			parentRefs: []gwtypes.ParentReference{
				{
					Name: gwtypes.ObjectName("gw-both"),
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					Namespace: "default", Name: "gw-both",
					Spec: gwtypes.GatewaySpec{
						Listeners: []gwtypes.Listener{
							{
								Name:     "http",
								Protocol: gwtypes.HTTPProtocolType,
								Port:     80,
							},
							{
								Name:     "https",
								Protocol: gwtypes.HTTPSProtocolType,
								Port:     443,
							},
						},
					},
				},
			},
			expectedProtocols: kong.StringSlice("grpc", "grpcs"),
		},
		{
			name: "missing gateway falls back to Kong Gateway default",
			parentRefs: []gwtypes.ParentReference{{
				Name: gwtypes.ObjectName("gw-missing"),
			}},
			expectedProtocols: nil,
		},
		{
			name: "sectionName filters listener protocol",
			parentRefs: []gwtypes.ParentReference{{
				Name:        gwtypes.ObjectName("gw-section"),
				SectionName: new(gwtypes.SectionName("https")),
			}},
			gateways: []*gwtypes.Gateway{
				{
					Namespace: "default", Name: "gw-section",
					Spec: gwtypes.GatewaySpec{
						Listeners: []gwtypes.Listener{
							{
								Name:     "http",
								Protocol: gwtypes.HTTPProtocolType,
								Port:     80,
							},
							{
								Name:     "https",
								Protocol: gwtypes.HTTPSProtocolType,
								Port:     443,
							},
						},
					},
				},
			},
			expectedProtocols: kong.StringSlice("grpcs"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakestore, err := store.NewFakeStore(store.FakeObjects{Gateways: tc.gateways})
			require.NoError(t, err)

			translator := mustNewTranslator(t, fakestore)
			grpcRoute := &gwtypes.GRPCRoute{
				Namespace: "default", Name: "grpcroute",
				Spec: gwtypes.GRPCRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{ParentRefs: tc.parentRefs},
				},
			}

			require.Equal(t, tc.expectedProtocols, translator.getProtocolsForKongRoute(grpcRoute))
		})
	}
}
