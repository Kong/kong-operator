package gateway

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
)

func TestIsRouteAttachedToReconciledGateway(t *testing.T) {
	type httpRouteTestCase struct {
		name           string
		objects        []client.Object
		route          *gatewayapi.HTTPRoute
		gatewayNN      controllers.OptionalNamespacedName
		expectedResult bool
	}

	kongGWClass := &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kong",
		},
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: "konghq.com/kic-gateway-controller",
		},
	}

	anotherGWClass := &gatewayapi.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "another",
		},
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: "another",
		},
	}

	kongGW := &gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kong",
			Namespace: "default",
		},
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: "kong",
		},
	}

	anotherGW := &gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another",
			Namespace: "default",
		},
		Spec: gatewayapi.GatewaySpec{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kong-httproute",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{
								Group:     new(gatewayapi.V1Group),
								Kind:      new(gatewayapi.Kind("Gateway")),
								Namespace: new(gatewayapi.Namespace("default")),
								Name:      gatewayapi.ObjectName("kong"),
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kong-httproute",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{
								Group:     new(gatewayapi.V1Group),
								Kind:      new(gatewayapi.Kind("Gateway")),
								Namespace: new(gatewayapi.Namespace("default")),
								Name:      gatewayapi.ObjectName("another"),
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kong-httproute",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{
								Group: new(gatewayapi.V1Group),
								Kind:  new(gatewayapi.Kind("Gateway")),
								Name:  gatewayapi.ObjectName("another"),
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kong-httproute",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{
								Kind: new(gatewayapi.Kind("Service")),
								Name: gatewayapi.ObjectName("kuma"),
							},
							{
								Group: new(gatewayapi.V1Group),
								Kind:  new(gatewayapi.Kind("Gateway")),
								Name:  gatewayapi.ObjectName("kong"),
							},
						},
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "parent ref pointing to non-exist gateway",
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kong-httproute",
					Namespace: "default",
				},
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{
								Group: new(gatewayapi.V1Group),
								Kind:  new(gatewayapi.Kind("Gateway")),
								Name:  gatewayapi.ObjectName("non-exist"),
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
			result := IsRouteAttachedToReconciledGateway[*gatewayapi.HTTPRoute](cl, logger, tc.gatewayNN, tc.route)
			require.Equal(t, tc.expectedResult, result)
		},
		)
	}
}

func TestRouteHasKongParentStatus(t *testing.T) {
	gwNamespace := gatewayapi.Namespace("default")
	otherNamespace := gatewayapi.Namespace("other-ns")

	testCases := []struct {
		name      string
		route     *gatewayapi.HTTPRoute
		gatewayNN controllers.OptionalNamespacedName
		expected  bool
	}{
		{
			name: "no gatewayNN set, route has our controller status - returns true",
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: "another-controller",
								ParentRef: gatewayapi.ParentReference{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
									Name:      "gateway-other",
									Namespace: &gwNamespace,
								},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
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
			route: &gatewayapi.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef: gatewayapi.ParentReference{
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
			result := routeHasKongParentStatus[*gatewayapi.HTTPRoute](tc.route, tc.gatewayNN)
			assert.Equal(t, tc.expected, result)
		})
	}
}
