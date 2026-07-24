package translator

import (
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
)

func TestIngressRulesFromTLSRoutes(t *testing.T) {
	tlsRouteTypeMeta := gatewayapi.TLSRouteTypeMeta
	kongVersionNotSupportingWildcardSNI := semver.MustParse("3.4.3")
	kongVersionSupportingWildcardSNI := semver.MustParse("3.7.0")

	mustCreateResourceFailure := func(message string, causingObjects ...client.Object) failures.ResourceFailure {
		failure, err := failures.NewResourceFailure(message, causingObjects...)
		require.NoError(t, err)
		return failure
	}

	tlsRouteExactHostname := &gatewayapi.TLSRoute{
		TypeMeta: tlsRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tlsroute-1",
		},
		Spec: gatewayapi.TLSRouteSpec{
			Hostnames: []gatewayapi.Hostname{
				"foo.com",
				"bar.com",
			},
			Rules: []gatewayapi.TLSRouteRule{
				{
					BackendRefs: []gatewayapi.BackendRef{
						builder.NewBackendRef("service1").WithPort(80).Build(),
						builder.NewBackendRef("service2").WithPort(443).Build(),
					},
				},
			},
		},
	}

	tlsRouteWildcardHostname := &gatewayapi.TLSRoute{
		TypeMeta: tlsRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tlsroute-1",
		},
		Spec: gatewayapi.TLSRouteSpec{
			Hostnames: []gatewayapi.Hostname{
				"*.foo.com",
			},
			Rules: []gatewayapi.TLSRouteRule{
				{
					BackendRefs: []gatewayapi.BackendRef{
						builder.NewBackendRef("service1").WithPort(80).Build(),
					},
				},
			},
		},
	}

	testCases := []struct {
		name                    string
		kongVersion             semver.Version
		expressionRoutesEnabled bool
		tlsRoutes               []*gatewayapi.TLSRoute
		services                []*corev1.Service
		expectedKongServices    []kongstate.Service
		expectedKongRoutes      map[string][]kongstate.Route
		expectedFailures        []failures.ResourceFailure
	}{
		{
			name:                    "tlsroute with single rule and exact hostnames",
			kongVersion:             kongVersionNotSupportingWildcardSNI,
			expressionRoutesEnabled: true,
			tlsRoutes:               []*gatewayapi.TLSRoute{tlsRouteExactHostname},
			// After https://github.com/Kong/kubernetes-ingress-controller/pull/5392
			// is merged the backendRef will be checked for existence in the store
			// so we need to add them here.
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
						Name: new("tlsroute.default.tlsroute-1.0"),
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
				"tlsroute.default.tlsroute-1.0": {
					{
						Route: kong.Route{
							Name:         new("tlsroute.default.tlsroute-1.0.0"),
							Expression:   new(`(tls.sni == "foo.com") || (tls.sni == "bar.com")`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tls"),
						},
						ExpressionRoutes: true,
					},
				},
			},
		},
		{
			name:        "tlsroute with wildcard hostname on Kong version not supporting it",
			kongVersion: kongVersionNotSupportingWildcardSNI,
			tlsRoutes:   []*gatewayapi.TLSRoute{tlsRouteWildcardHostname},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "service1",
					},
				},
			},
			expectedFailures: []failures.ResourceFailure{
				mustCreateResourceFailure("wildcard TLS SNIs are not supported in TLSRoute with Kong versions below 3.7.0", tlsRouteWildcardHostname),
			},
		},
		{
			name:                    "tlsroute with wildcard hostname on Kong version supporting it",
			kongVersion:             kongVersionSupportingWildcardSNI,
			expressionRoutesEnabled: true,
			tlsRoutes:               []*gatewayapi.TLSRoute{tlsRouteWildcardHostname},
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
						Name: new("tlsroute.default.tlsroute-1.0"),
					},
					Backends: []kongstate.ServiceBackend{
						builder.NewKongstateServiceBackend("service1").
							WithNamespace("default").
							WithPortNumber(80).
							MustBuild(),
					},
				},
			},
			expectedKongRoutes: map[string][]kongstate.Route{
				"tlsroute.default.tlsroute-1.0": {
					{
						Route: kong.Route{
							Name:         new("tlsroute.default.tlsroute-1.0.0"),
							Expression:   new(`tls.sni =^ ".foo.com"`),
							PreserveHost: new(true),
							Protocols:    kong.StringSlice("tls"),
						},
						ExpressionRoutes: true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakestore, err := store.NewFakeStore(store.FakeObjects{
				TLSRoutes: tc.tlsRoutes,
				Services:  tc.services,
			})
			require.NoError(t, err)
			translator := mustNewTranslator(t, fakestore)
			translator.kongVersion = tc.kongVersion
			translator.featureFlags.ExpressionRoutes = tc.expressionRoutesEnabled

			failureCollector := failures.NewResourceFailuresCollector(zapr.NewLogger(zap.NewNop()))
			translator.failuresCollector = failureCollector

			result := translator.ingressRulesFromTLSRoutes()
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
					require.Equal(t, expectedRoute.Expression, r.Expression)
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
