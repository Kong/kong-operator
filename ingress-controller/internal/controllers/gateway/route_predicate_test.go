package gateway

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestIsRouteAttachedToReconciledGateway(t *testing.T) {
	type httpRouteTestCase struct {
		name           string
		objects        []client.Object
		route          *gwtypes.HTTPRoute
		gatewayNN      controllers.OptionalNamespacedName
		expectedResult bool
	}

	kongGWClass := &gwtypes.GatewayClass{
		Name: "kong",
		Spec: gwtypes.GatewayClassSpec{
			ControllerName: "konghq.com/kic-gateway-controller",
		},
	}

	anotherGWClass := &gwtypes.GatewayClass{
		Name: "another",
		Spec: gwtypes.GatewayClassSpec{
			ControllerName: "another",
		},
	}

	kongGW := &gwtypes.Gateway{
		Name:      "kong",
		Namespace: "default",
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "kong",
		},
	}

	anotherGW := &gwtypes.Gateway{
		Name:      "another",
		Namespace: "default",
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "another",
		},
	}

	httpRouteTestCases := []httpRouteTestCase{
		{
			name: "single parent ref to gateway with expected class",
			objects: []client.Object{
				kongGW,
				kongGWClass,
			},
			route: &gwtypes.HTTPRoute{
				Name:      "kong-httproute",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group:     new(gwtypes.V1Group),
								Kind:      new(gwtypes.Kind("Gateway")),
								Namespace: new(gwtypes.Namespace("default")),
								Name:      gwtypes.ObjectName("kong"),
							},
						},
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "single parent ref to gateway with another class",
			objects: []client.Object{
				anotherGW,
				anotherGWClass,
			},
			route: &gwtypes.HTTPRoute{
				Name:      "kong-httproute",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group:     new(gwtypes.V1Group),
								Kind:      new(gwtypes.Kind("Gateway")),
								Namespace: new(gwtypes.Namespace("default")),
								Name:      gwtypes.ObjectName("another"),
							},
						},
					},
				},
			},
			expectedResult: false,
		},
		{
			name: "single parent ref to specified gateway",
			objects: []client.Object{
				anotherGW,
				anotherGWClass,
			},
			gatewayNN: controllers.NewOptionalNamespacedName(mo.Some(
				k8stypes.NamespacedName{
					Namespace: "default",
					Name:      "another",
				},
			)),
			route: &gwtypes.HTTPRoute{
				Name:      "kong-httproute",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: new(gwtypes.V1Group),
								Kind:  new(gwtypes.Kind("Gateway")),
								Name:  gwtypes.ObjectName("another"),
							},
						},
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "multiple parent refs with one pointing to reconciled gateway",
			objects: []client.Object{
				kongGW,
				kongGWClass,
			},
			route: &gwtypes.HTTPRoute{
				Name:      "kong-httproute",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Kind: new(gwtypes.Kind("Service")),
								Name: gwtypes.ObjectName("kuma"),
							},
							{
								Group: new(gwtypes.V1Group),
								Kind:  new(gwtypes.Kind("Gateway")),
								Name:  gwtypes.ObjectName("kong"),
							},
						},
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "parent ref pointing to non-exist gateway",
			route: &gwtypes.HTTPRoute{
				Name:      "kong-httproute",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: new(gwtypes.V1Group),
								Kind:  new(gwtypes.Kind("Gateway")),
								Name:  gwtypes.ObjectName("non-exist"),
							},
						},
					},
				},
			},
			expectedResult: true,
		},
	}

	for _, tc := range httpRouteTestCases {
		cl := fakeclient.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tc.objects...).Build()
		t.Run(tc.name, func(t *testing.T) {
			logger := logr.Discard()
			result := IsRouteAttachedToReconciledGateway[*gwtypes.HTTPRoute](cl, logger, tc.gatewayNN, tc.route)
			require.Equal(t, tc.expectedResult, result)
		},
		)
	}
}

func TestRouteHasKongParentStatus(t *testing.T) {
	gwNamespace := gwtypes.Namespace("default")
	otherNamespace := gwtypes.Namespace("other-ns")

	testCases := []struct {
		name      string
		route     *gwtypes.HTTPRoute
		gatewayNN controllers.OptionalNamespacedName
		expected  bool
	}{
		{
			name: "no gatewayNN set, route has our controller status - returns true",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-a",
									Namespace: &gwNamespace,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "no gatewayNN set, route has another controller status - returns false",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: "another-controller",
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-a",
									Namespace: &gwNamespace,
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "gatewayNN set, route has matching gateway status - returns true",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-a",
									Namespace: &gwNamespace,
								},
							},
						},
					},
				},
			},
			gatewayNN: controllers.NewOptionalNamespacedName(mo.Some(k8stypes.NamespacedName{
				Namespace: "default",
				Name:      "gateway-a",
			})),
			expected: true,
		},
		{
			name: "gatewayNN set, route has status for different gateway - returns false",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-b",
									Namespace: &otherNamespace,
								},
							},
						},
					},
				},
			},
			gatewayNN: controllers.NewOptionalNamespacedName(mo.Some(k8stypes.NamespacedName{
				Namespace: "default",
				Name:      "gateway-a",
			})),
			expected: false,
		},
		{
			name: "gatewayNN set, route has status for matching and non-matching gateways - returns true",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-other",
									Namespace: &gwNamespace,
								},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name:      "gateway-a",
									Namespace: &gwNamespace,
								},
							},
						},
					},
				},
			},
			gatewayNN: controllers.NewOptionalNamespacedName(mo.Some(k8stypes.NamespacedName{
				Namespace: "default",
				Name:      "gateway-a",
			})),
			expected: true,
		},
		{
			name: "gatewayNN set, parentRef namespace nil defaults to route namespace - returns true",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: "default",
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gwtypes.ParentReference{
									Name: "gateway-a",
									// Namespace is nil, defaults to route's namespace
								},
							},
						},
					},
				},
			},
			gatewayNN: controllers.NewOptionalNamespacedName(mo.Some(k8stypes.NamespacedName{
				Namespace: "default",
				Name:      "gateway-a",
			})),
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := routeHasKongParentStatus[*gwtypes.HTTPRoute](tc.route, tc.gatewayNN)
			assert.Equal(t, tc.expected, result)
		})
	}
}
