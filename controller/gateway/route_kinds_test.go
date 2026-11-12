package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

func TestDetectInstalledRouteKinds(t *testing.T) {
	gv := schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
	// Only TLSRoute and TCPRoute are served by the cluster.
	for _, kind := range []string{"TLSRoute", "TCPRoute"} {
		mapper.AddSpecific(
			gv.WithKind(kind),
			gv.WithResource(strings.ToLower(kind)+"s"),
			gv.WithResource(strings.ToLower(kind)),
			apimeta.RESTScopeNamespace,
		)
	}

	installed, err := detectInstalledRouteKinds(mapper)
	require.NoError(t, err)
	require.Equal(t, routeKindSet{
		"HTTPRoute": {},
		"TLSRoute":  {},
		"TCPRoute":  {},
	}, installed)
}

func routeKindsTestGateway() gwtypes.Gateway {
	from := gatewayv1.NamespacesFromSame
	allowed := &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: &from}}
	return gwtypes.Gateway{
		Name:      "gateway",
		Namespace: "default",
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80, AllowedRoutes: allowed},
				{Name: "tls", Protocol: gatewayv1.TLSProtocolType, Port: 443, AllowedRoutes: allowed},
			},
		},
	}
}

func TestCountAttachedRoutesWithMissingRouteCRDs(t *testing.T) {
	gw := routeKindsTestGateway()
	httpRoute := &gatewayv1.HTTPRoute{
		Name:      "route",
		Namespace: "default",
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{Name: "gateway"}},
			},
		},
	}

	// The cluster does not serve GRPCRoute and TLSRoute, as on OpenShift 4.19.
	missingKindsClient := func() client.Client {
		return fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&gw, httpRoute).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					switch list.(type) {
					case *gatewayv1.GRPCRouteList:
						return &apimeta.NoKindMatchError{GroupKind: schema.GroupKind{Group: gatewayv1.GroupName, Kind: "GRPCRoute"}}
					case *gatewayv1.TLSRouteList:
						return &apimeta.NoKindMatchError{GroupKind: schema.GroupKind{Group: gatewayv1.GroupName, Kind: "TLSRoute"}}
					}
					return c.List(ctx, list, opts...)
				},
			}).
			Build()
	}

	testCases := []struct {
		name           string
		installedKinds routeKindSet
	}{
		{
			name:           "missing CRDs not detected, list returns no match",
			installedKinds: nil,
		},
		{
			name:           "missing CRDs detected at startup",
			installedKinds: routeKindSet{"HTTPRoute": {}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := missingKindsClient()

			count, err := countAttachedRoutesForGatewayListener(t.Context(), &gw, gw.Spec.Listeners[0], cl, tc.installedKinds)
			require.NoError(t, err)
			require.Equal(t, int32(1), count, "HTTP listener counts the HTTPRoute and skips GRPCRoute")

			count, err = countAttachedRoutesForGatewayListener(t.Context(), &gw, gw.Spec.Listeners[1], cl, tc.installedKinds)
			require.NoError(t, err)
			require.Equal(t, int32(0), count, "TLS listener has no routes when TLSRoute is missing")
		})
	}
}

func TestSupportedKindsWithMissingRouteCRDs(t *testing.T) {
	group := gatewayv1.Group(gatewayv1.GroupName)
	httpOnly := routeKindSet{"HTTPRoute": {}}

	testCases := []struct {
		name               string
		listener           gatewayv1.Listener
		installedKinds     routeKindSet
		expectedKinds      []gatewayv1.RouteGroupKind
		expectedStatus     metav1.ConditionStatus
		expectedReason     gatewayv1.ListenerConditionReason
		expectedMsgContain string
	}{
		{
			name:           "HTTP listener without GRPCRoute reports HTTPRoute only",
			listener:       gatewayv1.Listener{Protocol: gatewayv1.HTTPProtocolType},
			installedKinds: httpOnly,
			expectedKinds:  []gatewayv1.RouteGroupKind{{Group: &group, Kind: "HTTPRoute"}},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: gatewayv1.ListenerReasonResolvedRefs,
		},
		{
			name:               "TLS listener without TLSRoute has unresolved refs",
			listener:           gatewayv1.Listener{Protocol: gatewayv1.TLSProtocolType},
			installedKinds:     httpOnly,
			expectedKinds:      []gatewayv1.RouteGroupKind{},
			expectedStatus:     metav1.ConditionFalse,
			expectedReason:     gatewayv1.ListenerReasonInvalidRouteKinds,
			expectedMsgContain: "missing CRDs: TLSRoute",
		},
		{
			name: "explicit TLSRoute kind without TLSRoute CRD has unresolved refs",
			listener: gatewayv1.Listener{
				Protocol:      gatewayv1.TLSProtocolType,
				AllowedRoutes: &gatewayv1.AllowedRoutes{Kinds: []gatewayv1.RouteGroupKind{{Group: &group, Kind: "TLSRoute"}}},
			},
			installedKinds:     httpOnly,
			expectedKinds:      []gatewayv1.RouteGroupKind{},
			expectedStatus:     metav1.ConditionFalse,
			expectedReason:     gatewayv1.ListenerReasonInvalidRouteKinds,
			expectedMsgContain: "Route TLSRoute CRD is not installed",
		},
		{
			name:           "nil set keeps all kinds",
			listener:       gatewayv1.Listener{Protocol: gatewayv1.TLSProtocolType},
			installedKinds: nil,
			expectedKinds:  []gatewayv1.RouteGroupKind{{Group: &group, Kind: "TLSRoute"}},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: gatewayv1.ListenerReasonResolvedRefs,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
			kinds, cond, err := getSupportedKindsWithResolvedRefsCondition(t.Context(), cl, referencegranthelpers.V1(),
				gatewayv1.Gateway{Namespace: "default"}, 1, tc.listener, tc.installedKinds)
			require.NoError(t, err)
			require.Equal(t, tc.expectedKinds, kinds)
			require.Equal(t, tc.expectedStatus, cond.Status)
			require.Equal(t, string(tc.expectedReason), cond.Reason)
			if tc.expectedMsgContain != "" {
				require.Contains(t, cond.Message, tc.expectedMsgContain)
			}
		})
	}
}

// Gateway API CRDs before v1.5 (OpenShift 4.19 to 4.22) require listener supportedKinds.
// If it is missing from the JSON, the API server rejects the whole Gateway status.
func TestListenerStatusAlwaysEncodesSupportedKinds(t *testing.T) {
	gw := routeKindsTestGateway()
	gw.Spec.Listeners = append(gw.Spec.Listeners, gatewayv1.Listener{
		Name: "custom", Protocol: "example.com/custom", Port: 8080, AllowedRoutes: gw.Spec.Listeners[0].AllowedRoutes,
	})
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(&gw).Build()
	installed := routeKindSet{"HTTPRoute": {}}

	aware := gatewayConditionsAndListenersAware(&gw)
	aware.initListenersStatus()
	aware.setConflicted()
	require.NoError(t, aware.setAcceptedAndAttachedRoutes(t.Context(), cl, installed))
	aware.initProgrammedAndListenersStatus()
	require.NoError(t, aware.setResolvedRefsAndSupportedKinds(t.Context(), cl, referencegranthelpers.V1(), installed))

	raw, err := json.Marshal(gw.Status)
	require.NoError(t, err)
	var status struct {
		Listeners []map[string]json.RawMessage `json:"listeners"`
	}
	require.NoError(t, json.Unmarshal(raw, &status))
	require.Len(t, status.Listeners, 3)
	for _, l := range status.Listeners {
		require.Contains(t, l, "supportedKinds", "listener %s", l["name"])
	}
}
