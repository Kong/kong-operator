package gateway

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGetListenerSupportedRouteKinds(t *testing.T) {
	testCases := []struct {
		name                   string
		listener               gwtypes.Listener
		expectedSupportedKinds []gwtypes.RouteGroupKind
		resolvedRefsReason     gwtypes.ListenerConditionReason
	}{
		{
			name: "only HTTP protocol specified",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPProtocolType,
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				builder.NewRouteGroupKind().HTTPRoute().Build(),
				builder.NewRouteGroupKind().GRPCRoute().Build(),
			},
			resolvedRefsReason: gwtypes.ListenerReasonResolvedRefs,
		},
		{
			name: "only HTTPS protocol specified",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPSProtocolType,
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				builder.NewRouteGroupKind().HTTPRoute().Build(),
				builder.NewRouteGroupKind().GRPCRoute().Build(),
			},
			resolvedRefsReason: gwtypes.ListenerReasonResolvedRefs,
		},
		{
			name: "only TCP protocol specified",
			listener: gwtypes.Listener{
				Protocol: gwtypes.TCPProtocolType,
			},
			expectedSupportedKinds: builder.NewRouteGroupKind().TCPRoute().IntoSlice(),
			resolvedRefsReason:     gwtypes.ListenerReasonResolvedRefs,
		},
		{
			name: "only UDP protocol specified",
			listener: gwtypes.Listener{
				Protocol: gwtypes.UDPProtocolType,
			},
			expectedSupportedKinds: builder.NewRouteGroupKind().UDPRoute().IntoSlice(),
			resolvedRefsReason:     gwtypes.ListenerReasonResolvedRefs,
		},
		{
			name: "only TLS protocol specified",
			listener: gwtypes.Listener{
				Protocol: gwtypes.TLSProtocolType,
			},
			expectedSupportedKinds: builder.NewRouteGroupKind().TLSRoute().IntoSlice(),
			resolvedRefsReason:     gwtypes.ListenerReasonResolvedRefs,
		},
		{
			name: "Kind not included in global gets discarded",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPProtocolType,
				AllowedRoutes: &gwtypes.AllowedRoutes{
					Kinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.Group("unknown.group.com")),
							Kind:  gwtypes.Kind("UnknownKind"),
						},
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: new(gwtypes.V1Group),
					Kind:  gwtypes.Kind("HTTPRoute"),
				},
			},
			resolvedRefsReason: gwtypes.ListenerReasonInvalidRouteKinds,
		},
		{
			name: "Kind included in global gets passed",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPProtocolType,
				AllowedRoutes: &gwtypes.AllowedRoutes{
					Kinds: builder.NewRouteGroupKind().HTTPRoute().IntoSlice(),
				},
			},
			expectedSupportedKinds: builder.NewRouteGroupKind().HTTPRoute().IntoSlice(),
			resolvedRefsReason:     gwtypes.ListenerReasonResolvedRefs,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := getListenerSupportedRouteKinds(tc.listener)
			require.Equal(t, tc.expectedSupportedKinds, got)
			require.Equal(t, tc.resolvedRefsReason, reason)
		})
	}
}

func TestGetListenerStatus(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, gatewayv1.Install(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	testCases := []struct {
		name                     string
		gateway                  *gwtypes.Gateway
		kongListens              []gwtypes.Listener
		expectedListenerStatuses []gwtypes.ListenerStatus
	}{
		{
			name: "only one listener",
			gateway: &gwtypes.Gateway{
				TypeMeta: gwtypes.V1GatewayTypeMeta,
				Name:     "single-listener",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gwtypes.Listener{
						{
							Name:     "tcp-80",
							Port:     80,
							Protocol: gwtypes.TCPProtocolType,
						},
					},
				},
			},
			kongListens: []gwtypes.Listener{
				{
					Port:     80,
					Protocol: gwtypes.TCPProtocolType,
				},
			},
			expectedListenerStatuses: []gwtypes.ListenerStatus{
				{
					Name: gwtypes.SectionName("tcp-80"),
					Conditions: []metav1.Condition{
						{
							Type:   string(gwtypes.ListenerConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		},
		{
			name: "only one listener without a matching protocol or port",
			gateway: &gwtypes.Gateway{
				TypeMeta: gwtypes.V1GatewayTypeMeta,
				Name:     "single-listener",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gwtypes.Listener{
						{
							Name:     "tcp-80",
							Port:     80,
							Protocol: gwtypes.TLSProtocolType,
						},
					},
				},
			},
			kongListens: []gwtypes.Listener{
				{
					Port:     80,
					Protocol: gwtypes.TCPProtocolType,
				},
			},
			expectedListenerStatuses: []gwtypes.ListenerStatus{
				{
					Name: gwtypes.SectionName("tcp-80"),
					Conditions: []metav1.Condition{
						{
							Type:   string(gwtypes.ListenerConditionAccepted),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
		{
			name: "2 listeners, 1 with a matching protocol",
			gateway: &gwtypes.Gateway{
				TypeMeta: gwtypes.V1GatewayTypeMeta,
				Name:     "single-listener",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gwtypes.Listener{
						{
							Name:     "tls-443",
							Port:     443,
							Protocol: gwtypes.TLSProtocolType,
						},
						{
							Name:     "tcp-80",
							Port:     80,
							Protocol: gwtypes.TCPProtocolType,
						},
					},
				},
			},
			kongListens: []gwtypes.Listener{
				{
					Port:     80,
					Protocol: gwtypes.TCPProtocolType,
				},
			},
			expectedListenerStatuses: []gwtypes.ListenerStatus{
				{
					Name: gwtypes.SectionName("tcp-80"),
					Conditions: []metav1.Condition{
						{
							Type:   string(gwtypes.ListenerConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
				{
					Name: gwtypes.SectionName("tls-443"),
					Conditions: []metav1.Condition{
						{
							Type:   string(gwtypes.ListenerConditionAccepted),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			statuses, err := getListenerStatus(ctx, tc.gateway, tc.kongListens, nil, client)
			require.NoError(t, err)
			require.Len(t, statuses, len(tc.expectedListenerStatuses), "should return expected number of listener statused")
			for _, expectedListenerStatus := range tc.expectedListenerStatuses {
				listenerStatus, ok := lo.Find(statuses, func(ls gwtypes.ListenerStatus) bool {
					return ls.Name == expectedListenerStatus.Name
				})
				require.Truef(t, ok, "should find listener status of listener %s", expectedListenerStatus.Name)
				assertOnlyOneConditionForType(t, listenerStatus.Conditions)
				for _, expectedCondition := range expectedListenerStatus.Conditions {
					assert.Truef(t,
						lo.ContainsBy(listenerStatus.Conditions, func(c metav1.Condition) bool {
							return c.Type == expectedCondition.Type && c.Status == expectedCondition.Status
						}),
						"Condition %q should have Status %q: found listener conditions:\n%#v",
						expectedCondition.Type, expectedCondition.Status, listenerStatus.Conditions,
					)
				}
			}
		})
	}
}

func TestGetAttachedRoutesForListener(t *testing.T) {
	group := gwtypes.V1Group
	kind := gwtypes.Kind("Gateway")
	sectionName := gwtypes.SectionName("http")
	gatewayName := "my-gateway"
	myGateway := gwtypes.Gateway{
		Name:      gatewayName,
		Namespace: "default",
		Spec: gwtypes.GatewaySpec{
			Listeners: []gwtypes.Listener{{
				Name:     sectionName,
				Port:     80,
				Protocol: gwtypes.HTTPProtocolType,
			}},
		},
		Status: gwtypes.GatewayStatus{
			Listeners: []gwtypes.ListenerStatus{
				{
					Name: sectionName,
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
				},
			},
		},
	}

	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, gatewayv1.Install(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	generateHTTPRoute := func(gateways []gwtypes.ObjectName) gwtypes.HTTPRoute {
		var parentRefs []gwtypes.ParentReference
		for _, gateway := range gateways {
			parentRefs = append(parentRefs, gwtypes.ParentReference{
				Group: &group,
				Kind:  &kind,
				Name:  gateway,
			})
		}
		var parentRefsStatus []gwtypes.RouteParentStatus
		for _, gateway := range gateways {
			parentRefsStatus = append(parentRefsStatus, gwtypes.RouteParentStatus{
				ParentRef: gwtypes.ParentReference{
					Group: &group,
					Kind:  &kind,
					Name:  gateway,
				},
			})
		}
		// Create a single HTTPRoute with the specified parentRefs
		return gwtypes.HTTPRoute{
			GenerateName: "http-1-",
			Namespace:    "default",
			Spec: gwtypes.HTTPRouteSpec{
				CommonRouteSpec: gwtypes.CommonRouteSpec{
					ParentRefs: parentRefs,
				},
			},
			Status: gwtypes.HTTPRouteStatus{
				RouteStatus: gwtypes.RouteStatus{
					Parents: parentRefsStatus,
				},
			},
		}
	}

	tests := []struct {
		name          string
		routes        []gwtypes.HTTPRoute
		listenerIndex int
		want          int32
	}{
		{
			name: "single route with single parentRef",
			routes: []gwtypes.HTTPRoute{
				generateHTTPRoute(
					[]gwtypes.ObjectName{gwtypes.ObjectName(gatewayName)},
				),
			},
			listenerIndex: 0,
			want:          1,
		},
		{
			name: "single route with multiple parentRefs",
			routes: []gwtypes.HTTPRoute{
				generateHTTPRoute(
					[]gwtypes.ObjectName{
						gwtypes.ObjectName(gatewayName), "other-gateway",
					},
				),
			},
			listenerIndex: 0,
			want:          1,
		},
		{
			name: "multiple routes with multiple parentRefs",
			routes: []gwtypes.HTTPRoute{
				generateHTTPRoute(
					[]gwtypes.ObjectName{
						gwtypes.ObjectName(gatewayName), "other-gateway",
					},
				),
				generateHTTPRoute(
					[]gwtypes.ObjectName{
						gwtypes.ObjectName(gatewayName), "other-gateway",
					},
				),
			},
			listenerIndex: 0,
			want:          2,
		},
		{
			name: "route not accepted by gateway",
			routes: []gwtypes.HTTPRoute{
				generateHTTPRoute(
					[]gwtypes.ObjectName{
						"other-gateway",
					},
				),
			},
			listenerIndex: 0,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, route := range tt.routes {
				require.NoError(t, client.Create(ctx, &route))
			}

			got, err := getAttachedRoutesForListener(ctx, client, myGateway, tt.listenerIndex)

			// Tear down the created routes
			require.NoError(t, client.DeleteAllOf(ctx, &gwtypes.HTTPRoute{}))

			// Check that the number of routes returned matches the expected count
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func assertOnlyOneConditionForType(t *testing.T, conditions []metav1.Condition) {
	conditionsNum := lo.CountValuesBy(conditions, func(c metav1.Condition) string {
		return c.Type
	})
	for c, n := range conditionsNum {
		assert.Equalf(t, 1, n, "condition %s occurred %d times - expected 1 occurrence", c, n)
	}
}

func TestRouteAcceptedByGateways(t *testing.T) {
	testCases := []struct {
		name               string
		routeNamespace     string
		route              *gwtypes.HTTPRoute
		gateways           []client.Object
		expectedGatewayNNs []k8stypes.NamespacedName
	}{
		{
			name:           "returns the gateway regardless of the route parent status conditions",
			routeNamespace: "default",
			route: &gwtypes.HTTPRoute{
				Namespace: "default",
				Name:      "route-1",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ParentRef: gwtypes.ParentReference{
									Name: "gateway-1",
								},
							},
						},
					},
				},
			},
			gateways: []client.Object{
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "gateway-1",
					Spec: gwtypes.GatewaySpec{
						Listeners: builder.NewListener("http").WithPort(8080).HTTP().IntoSlice(),
					},
					Status: gwtypes.GatewayStatus{
						Listeners: []gwtypes.ListenerStatus{
							{
								Name: "http",
								SupportedKinds: []gwtypes.RouteGroupKind{
									{
										Group: new(gwtypes.V1Group),
										Kind:  gwtypes.Kind("HTTPRoute"),
									},
								},
							},
						},
					},
				},
			},
			expectedGatewayNNs: []k8stypes.NamespacedName{
				{
					Namespace: "default",
					Name:      "gateway-1",
				},
			},
		},
		{
			name:           "returns the gateway regardless of the route parent status conditions",
			routeNamespace: "default",
			route: &gwtypes.HTTPRoute{
				Namespace: "default",
				Name:      "route-1",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ParentRef: gwtypes.ParentReference{
									Name:  "gateway-1",
									Group: new(gwtypes.Group("wrong-group")),
								},
								Conditions: []metav1.Condition{
									{
										Status: metav1.ConditionTrue,
										Type:   string(gwtypes.RouteConditionAccepted),
									},
								},
							},
							{
								ParentRef: gwtypes.ParentReference{
									Name: "gateway-2",
									Kind: new(gwtypes.Kind("wrong-kind")),
								},
								Conditions: []metav1.Condition{
									{
										Status: metav1.ConditionTrue,
										Type:   string(gwtypes.RouteConditionAccepted),
									},
								},
							},
							{
								ParentRef: gwtypes.ParentReference{
									Name: "gateway-3",
								},
								Conditions: []metav1.Condition{
									{
										Status: metav1.ConditionTrue,
										Type:   string(gwtypes.RouteConditionAccepted),
									},
								},
							},
							{
								ParentRef: gwtypes.ParentReference{
									Name: "gateway-4",
								},
								Conditions: []metav1.Condition{
									{
										Status: metav1.ConditionFalse,
										Type:   string(gwtypes.RouteConditionAccepted),
									},
								},
							},
						},
					},
				},
			},
			gateways: []client.Object{
				&gwtypes.Gateway{
					APIVersion: "wrong-group/v1",
					Kind:       "Gateway",
					Namespace:  "default",
					Name:       "gateway-1",
					Spec: gwtypes.GatewaySpec{
						Listeners: builder.NewListener("http").WithPort(8080).HTTP().IntoSlice(),
					},
					Status: gwtypes.GatewayStatus{
						Listeners: []gwtypes.ListenerStatus{
							{
								Name: "http",
								SupportedKinds: []gwtypes.RouteGroupKind{
									{
										Group: new(gwtypes.V1Group),
										Kind:  gwtypes.Kind("HTTPRoute"),
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					APIVersion: gwtypes.GroupVersion.String(),
					Kind:       "wrong-kind",
					Namespace:  "default",
					Name:       "gateway-2",
					Spec: gwtypes.GatewaySpec{
						Listeners: builder.NewListener("http").WithPort(8080).HTTP().IntoSlice(),
					},
					Status: gwtypes.GatewayStatus{
						Listeners: []gwtypes.ListenerStatus{
							{
								Name: "http",
								SupportedKinds: []gwtypes.RouteGroupKind{
									{
										Group: new(gwtypes.V1Group),
										Kind:  gwtypes.Kind("HTTPRoute"),
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "gateway-3",
					Spec: gwtypes.GatewaySpec{
						Listeners: builder.NewListener("http").WithPort(8080).HTTP().IntoSlice(),
					},
					Status: gwtypes.GatewayStatus{
						Listeners: []gwtypes.ListenerStatus{
							{
								Name: "http",
								SupportedKinds: []gwtypes.RouteGroupKind{
									{
										Group: new(gwtypes.V1Group),
										Kind:  gwtypes.Kind("HTTPRoute"),
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "gateway-4",
					Spec: gwtypes.GatewaySpec{
						Listeners: builder.NewListener("http").WithPort(8080).HTTP().IntoSlice(),
					},
					Status: gwtypes.GatewayStatus{
						Listeners: []gwtypes.ListenerStatus{
							{
								Name: "http",
								SupportedKinds: []gwtypes.RouteGroupKind{
									{
										Group: new(gwtypes.V1Group),
										Kind:  gwtypes.Kind("HTTPRoute"),
									},
								},
							},
						},
					},
				},
			},
			expectedGatewayNNs: []k8stypes.NamespacedName{
				{
					Namespace: "default",
					Name:      "gateway-3",
				},
				{
					Namespace: "default",
					Name:      "gateway-4",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gateways := routeAcceptedByGateways(tc.route)
			assert.Equal(t, tc.expectedGatewayNNs, gateways)
		})
	}
}

func Test_isEqualListenersStatus(t *testing.T) {
	lastTransitionTime := metav1.Now()
	lastTransitionTime2 := metav1.NewTime(lastTransitionTime.Add(time.Second))

	tests := []struct {
		name     string
		a        []gwtypes.ListenerStatus
		b        []gwtypes.ListenerStatus
		expected bool
	}{
		{
			name: "equal listeners status",
			a: []gwtypes.ListenerStatus{
				{
					Name: "http",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
				},
				{
					Name: "https",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Accepted",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: lastTransitionTime,
						},
					},
				},
			},
			b: []gwtypes.ListenerStatus{
				{
					Name: "https",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Accepted",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: lastTransitionTime2,
						},
					},
				},
				{
					Name: "http",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "not equal listeners status",
			a: []gwtypes.ListenerStatus{
				{
					Name: "http",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "Programmed",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			b: []gwtypes.ListenerStatus{
				{
					Name: "http",
					SupportedKinds: []gwtypes.RouteGroupKind{
						{
							Group: new(gwtypes.V1Group),
							Kind:  gwtypes.Kind("HTTPRoute"),
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "Accepted",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name:     "no listeners",
			a:        []gwtypes.ListenerStatus{},
			b:        []gwtypes.ListenerStatus{},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, isEqualListenersStatus(tt.a, tt.b))
		})
	}
}
