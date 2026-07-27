package translator

import (
	"strings"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator/subtranslator"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
)

// tcpProgrammedStatus builds Gateway.Status.Listeners entries that mark each
// named listener as Programmed and accepting TCPRoute - the status a real
// GatewayReconciler would have written by the time the Gateway is pushed
// into the translator's cache, and what listener-attachment arbitration now
// requires.
func tcpProgrammedStatus(listenerNames ...string) gatewayapi.GatewayStatus {
	kinds := builder.NewRouteGroupKind().TCPRoute().IntoSlice()
	statuses := make([]gatewayapi.ListenerStatus, 0, len(listenerNames))
	for _, name := range listenerNames {
		statuses = append(statuses, gatewayapi.ListenerStatus{
			Name:           gatewayapi.SectionName(name),
			SupportedKinds: kinds,
			Conditions: []metav1.Condition{{
				Type:   string(gatewayapi.ListenerConditionProgrammed),
				Status: metav1.ConditionTrue,
			}},
		})
	}
	return gatewayapi.GatewayStatus{Listeners: statuses}
}

func TestIngressRulesFromTCPRoutesUsingExpressionRoutes(t *testing.T) {
	tcpRouteTypeMeta := metav1.TypeMeta{Kind: "TCPRoute", APIVersion: gatewayv1.GroupVersion.String()}

	testCases := []struct {
		name                 string
		gateways             []*gatewayapi.Gateway
		tcpRoutes            []*gatewayapi.TCPRoute
		services             []*corev1.Service
		expectedKongServices []kongstate.Service
		expectedKongRoutes   map[string][]kongstate.Route
		expectedFailures     []failures.ResourceFailure
	}{
		{
			name: "TCPRoute with single rule, single backendref and Gateway in the same namespace",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "default",
					},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{
							builder.NewListener("tcp80").WithPort(80).TCP().Build(),
							builder.NewListener("udp80").WithPort(80).UDP().Build(),
						},
					},
					Status: tcpProgrammedStatus("tcp80"),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "test-1",
					},
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tcproute-1",
						Namespace: "default",
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name: "gateway-1",
								},
							},
						},
						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service1").WithPort(8080).Build(),
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name: new("tcproute.default.tcproute-1.0"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").WithPortNumber(8080).MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.tcproute-1.0": {
					{
						Route: kong.Route{
							Name:         new("tcproute.default.tcproute-1.0.0"),
							Expression:   new(`net.dst.port == 80`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tcp"),
						},
						ExpressionRoutes: true,
					},
				},
			},
		},
		{
			name: "TCPRoute with single rule, multiple backendrefs and Gateway in the different namespace",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "test-1",
					},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{
							builder.NewListener("tcp80").WithPort(80).TCP().Build(),
							builder.NewListener("tcp443").WithPort(443).TCP().Build(),
						},
					},
					Status: tcpProgrammedStatus("tcp80", "tcp443"),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "default",
					},
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tcproute-1",
						Namespace: "default",
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name:      "gateway-1",
									Namespace: new(gatewayapi.Namespace("test-1")),
								},
							},
						},
						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service1").WithPort(80).Build(),
									builder.NewBackendRef("service2").WithPort(443).Build(),
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service2",
					},
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name: new("tcproute.default.tcproute-1.0"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(), builder.NewKongstateServiceBackend("service2").WithPortNumber(443).MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.tcproute-1.0": {
					{
						Route: kong.Route{
							Name:         new("tcproute.default.tcproute-1.0.0"),
							Expression:   new(`(net.dst.port == 80) || (net.dst.port == 443)`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tcp"),
						},
						ExpressionRoutes: true,
					},
				},
			},
		},
		{
			name: "TCPRoute with multiple rules and multiple Gateways with multiple listeners and sectionName for Gateway specified",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "default",
					},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{
							{
								Name:     "tcp80",
								Port:     80,
								Protocol: gatewayapi.TCPProtocolType,
							},
							{
								Name:     "tcp443",
								Port:     443,
								Protocol: gatewayapi.TCPProtocolType,
							},
							{
								Name:     "tcp8080",
								Port:     8080,
								Protocol: gatewayapi.TCPProtocolType,
							},
							{
								Name:     "tcp8443",
								Port:     8443,
								Protocol: gatewayapi.TCPProtocolType,
							},
						},
					},
					Status: tcpProgrammedStatus("tcp80", "tcp443", "tcp8080", "tcp8443"),
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tcproute-1",
						Namespace: "default",
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp80")),
								},
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp443")),
								},
							},
						},

						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service1").WithPort(80).Build(),
									builder.NewBackendRef("service2").WithPort(443).Build(),
								},
							},
						},
					},
				},
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tcproute-2",
						Namespace: "default",
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp8080")),
								},
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp8443")),
								},
							},
						},

						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service3").WithPort(8080).Build(),
									builder.NewBackendRef("service4").WithPort(8443).Build(),
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service4",
					},
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name: new("tcproute.default.tcproute-1.0"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(), builder.NewKongstateServiceBackend("service2").WithPortNumber(443).MustBuild(),
					},
				},
				{
					Service: kong.Service{
						Name: new("tcproute.default.tcproute-2.0"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service3").WithPortNumber(8080).MustBuild(),
						builder.NewKongstateServiceBackend("service4").WithPortNumber(8443).MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.tcproute-1.0": {
					{
						Route: kong.Route{
							Name:         new("tcproute.default.tcproute-1.0.0"),
							Expression:   new(`(net.dst.port == 80) || (net.dst.port == 443)`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tcp"),
						},
						ExpressionRoutes: true,
					},
				},
				"tcproute.default.tcproute-2.0": {
					{
						Route: kong.Route{
							Name:         new("tcproute.default.tcproute-2.0.0"),
							Expression:   new(`(net.dst.port == 8080) || (net.dst.port == 8443)`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tcp"),
						},
						ExpressionRoutes: true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeStore, err := store.NewFakeStore(store.FakeObjects{
				Gateways:  tc.gateways,
				TCPRoutes: tc.tcpRoutes,
				Services:  tc.services,
			})
			require.NoError(t, err)
			translator := mustNewTranslator(t, fakeStore)
			translator.featureFlags.ExpressionRoutes = true

			failureCollector := failures.NewResourceFailuresCollector(zapr.NewLogger(zap.NewNop()))
			translator.failuresCollector = failureCollector

			result := translator.ingressRulesFromTCPRoutes()
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
					require.Equalf(t, expectedRoute.Expression, r.Expression, "route %s should have expected expression", *routeName)
					require.Equal(t, expectedRoute.Protocols, r.Protocols)
				}
			}
			// check translation failures
			translationFailures := failureCollector.PopResourceFailures()
			require.Len(t, translationFailures, len(tc.expectedFailures))
			for _, expectedTranslationFailure := range tc.expectedFailures {
				expectedFailureMessage := expectedTranslationFailure.Message()
				require.True(t, lo.ContainsBy(translationFailures, func(failure failures.ResourceFailure) bool {
					return strings.Contains(failure.Message(), expectedFailureMessage)
				}))
			}
		})
	}
}

func TestIngressRulesFromTCPRoutes(t *testing.T) {
	tcpRouteTypeMeta := metav1.TypeMeta{Kind: "TCPRoute", APIVersion: gatewayv1.GroupVersion.String()}

	testCases := []struct {
		name                 string
		gateways             []*gatewayapi.Gateway
		tcpRoutes            []*gatewayapi.TCPRoute
		services             []*corev1.Service
		expectedKongServices []kongstate.Service
		expectedKongRoutes   map[string][]kongstate.Route
		expectedFailures     []failures.ResourceFailure
	}{
		{
			name: "single TCPRoute with single rule, single backendref and Gateway in the same namespace",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "default",
					},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{
							builder.NewListener("tcp80").WithPort(80).TCP().Build(),
							builder.NewListener("udp80").WithPort(80).UDP().Build(),
						},
					},
					Status: tcpProgrammedStatus("tcp80"),
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-rule",
						Namespace: "default",
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name: "gateway-1",
								},
							},
						},
						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service1").WithPort(8080).Build(),
								},
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name:     new("tcproute.default.single-rule.0"),
						Protocol: new("tcp"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(8080).
							MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.single-rule.0": {
					{
						Route: kong.Route{
							Name: new("tcproute.default.single-rule.0.0"),
							Destinations: []*kong.CIDRPort{
								{Port: new(80)},
							},
							Protocols: kong.StringSlice("tcp"),
						},
					},
				},
			},
		},
		{
			name: "two TCPRoutes on the same listener — only the older one wins",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "tcp-gw"},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{{
							Name:     "tcp",
							Protocol: gatewayapi.TCPProtocolType,
							Port:     9999,
						}},
					},
					Status: tcpProgrammedStatus("tcp"),
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         "default",
						Name:              "older",
						CreationTimestamp: metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayv1.ParentReference{{
								Name: gatewayv1.ObjectName("tcp-gw"),
							}},
						},
						Rules: []gatewayapi.TCPRouteRule{{
							BackendRefs: []gatewayv1.BackendRef{{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "svc-older",
									Kind: new(gatewayv1.Kind("Service")),
									Port: new(gatewayv1.PortNumber(8080)),
								},
							}},
						}},
					},
				},
				{
					TypeMeta: tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         "default",
						Name:              "newer",
						CreationTimestamp: metav1.NewTime(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)),
					},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayv1.ParentReference{{
								Name: gatewayv1.ObjectName("tcp-gw"),
							}},
						},
						Rules: []gatewayapi.TCPRouteRule{{
							BackendRefs: []gatewayv1.BackendRef{{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "svc-newer",
									Kind: new(gatewayv1.Kind("Service")),
									Port: new(gatewayv1.PortNumber(9090)),
								},
							}},
						}},
					},
				},
			},
			services: []*corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "svc-older"}},
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "svc-newer"}},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name:     new("tcproute.default.older.0"),
						Protocol: new("tcp"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("svc-older").
							WithNamespace("default").
							WithPortNumber(8080).
							MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.older.0": {
					{
						Route: kong.Route{
							Name:         new("tcproute.default.older.0.0"),
							Destinations: []*kong.CIDRPort{{Port: new(9999)}},
							Protocols:    kong.StringSlice("tcp"),
						},
					},
				},
			},
		},
		{
			name: "multiple TCPRoutes with translation errors",
			gateways: []*gatewayapi.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-1",
						Namespace: "default",
					},
					Spec: gatewayapi.GatewaySpec{
						Listeners: []gatewayapi.Listener{
							builder.NewListener("tcp80").WithPort(80).TCP().Build(),
							builder.NewListener("tcp8080").WithPort(8080).TCP().Build(),
						},
					},
					Status: tcpProgrammedStatus("tcp80", "tcp8080"),
				},
			},
			tcpRoutes: []*gatewayapi.TCPRoute{
				{
					TypeMeta:   tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{Name: "single-rule", Namespace: "default"},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp80")),
								},
							},
						},
						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service1").WithPort(80).Build(),
								},
							},
						},
					},
				},
				{
					TypeMeta:   tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{Name: "single-rule-2", Namespace: "default"},
					Spec: gatewayapi.TCPRouteSpec{
						CommonRouteSpec: gatewayapi.CommonRouteSpec{
							ParentRefs: []gatewayapi.ParentReference{
								{
									Name:        "gateway-1",
									SectionName: new(gatewayapi.SectionName("tcp8080")),
								},
							},
						},
						Rules: []gatewayapi.TCPRouteRule{
							{
								BackendRefs: []gatewayapi.BackendRef{
									builder.NewBackendRef("service2").WithPort(8080).Build(),
								},
							},
						},
					},
				},
				{
					TypeMeta:   tcpRouteTypeMeta,
					ObjectMeta: metav1.ObjectMeta{Name: "no-rule", Namespace: "default"},
					Spec:       gatewayapi.TCPRouteSpec{},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service2",
					},
				},
			},
			expectedKongServices: []kongstate.Service{
				{
					Service: kong.Service{
						Name:     new("tcproute.default.single-rule.0"),
						Protocol: new("tcp"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
				{
					Service: kong.Service{
						Name:     new("tcproute.default.single-rule-2.0"),
						Protocol: new("tcp"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service2").WithPortNumber(8080).MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tcproute.default.single-rule.0": {
					{
						Route: kong.Route{
							Name: new("tcproute.default.single-rule.0.0"),
							Destinations: []*kong.CIDRPort{
								{Port: new(80)},
							},
							Protocols: kong.StringSlice("tcp"),
						},
					},
				},
				"tcproute.default.single-rule-2.0": {
					{
						Route: kong.Route{
							Name: new("tcproute.default.single-rule-2.0.0"),
							Destinations: []*kong.CIDRPort{
								{Port: new(8080)},
							},
							Protocols: kong.StringSlice("tcp"),
						},
					},
				},
			},
			expectedFailures: []failures.ResourceFailure{
				newResourceFailure(
					t, subtranslator.ErrRouteValidationNoRules.Error(),
					&gatewayapi.TCPRoute{
						TypeMeta:   tcpRouteTypeMeta,
						ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "no-rule"},
					},
				),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeStore, err := store.NewFakeStore(store.FakeObjects{
				Gateways:  tc.gateways,
				TCPRoutes: tc.tcpRoutes,
				Services:  tc.services,
			})
			require.NoError(t, err)
			translator := mustNewTranslator(t, fakeStore)

			failureCollector := failures.NewResourceFailuresCollector(zapr.NewLogger(zap.NewNop()))
			translator.failuresCollector = failureCollector

			result := translator.ingressRulesFromTCPRoutes()
			// check services
			require.Len(t, result.ServiceNameToServices, len(tc.expectedKongServices),
				"should have expected number of services")
			for _, expectedKongService := range tc.expectedKongServices {
				kongService, ok := result.ServiceNameToServices[*expectedKongService.Name]
				require.Truef(t, ok, "should find service %s", *expectedKongService.Name)
				require.Equalf(t, expectedKongService.Backends, kongService.Backends,
					"service %s should have expected backends", *expectedKongService.Name)
				require.Equalf(t, "tcp", *kongService.Protocol, "service %s should use TCP protocol", *expectedKongService.Name)
				// check routes
				expectedKongRoutes := tc.expectedKongRoutes[*kongService.Name]
				require.Lenf(t, kongService.Routes, len(expectedKongRoutes),
					"service %s should have expected number of routes", *expectedKongService.Name)

				kongRouteNameToRoute := lo.SliceToMap(kongService.Routes, func(r kongstate.Route) (string, kongstate.Route) {
					return *r.Name, r
				})
				for _, expectedRoute := range expectedKongRoutes {
					routeName := expectedRoute.Name
					r, ok := kongRouteNameToRoute[*routeName]
					require.Truef(t, ok, "should find route %s", *routeName)
					require.Equalf(t, expectedRoute.Destinations, r.Destinations, "route %s should have expected destinations", *routeName)
					require.Equalf(t, expectedRoute.Protocols, r.Protocols, "route %s should have expected protocols", *routeName)
				}
			}
			// check translation failures
			translationFailures := failureCollector.PopResourceFailures()
			require.Len(t, translationFailures, len(tc.expectedFailures))
			for _, expectedTranslationFailure := range tc.expectedFailures {
				expectedFailureMessage := expectedTranslationFailure.Message()
				require.True(t, lo.ContainsBy(translationFailures, func(failure failures.ResourceFailure) bool {
					return strings.Contains(failure.Message(), expectedFailureMessage)
				}))
			}
		})
	}
}
