package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestListHTTPRoutesForGateway(t *testing.T) {
	testCases := []struct {
		name        string
		httpRoutes  []client.Object
		gateway     *gwtypes.Gateway
		expected    []gwtypes.HTTPRoute
		expectedErr bool
	}{
		{
			name: "returns HTTPRoute for a Gateway",
			httpRoutes: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: []gwtypes.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "does not return HTTPRoute for a Gateway when it is not a parent",
			httpRoutes: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: nil,
		},
		{
			name: "returns HTTPRoute when section name does match",
			httpRoutes: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("http")),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			expected: []gwtypes.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("http")),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "does not return HTTPRoute when section name does not match",
			httpRoutes: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("http-1")),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "does not return HTTPRoute when port does not match",
			httpRoutes: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "http-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("http")),
									Port:        new(gwtypes.PortNumber(8080)),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.gateway).
				WithObjects(tc.httpRoutes...).
				Build()
			routes, err := ListHTTPRoutesForGateway(t.Context(), cl, tc.gateway)
			if tc.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, tc.expected, routes)
			}
		})
	}
}

func TestListGRPCRoutesForGateway(t *testing.T) {
	testCases := []struct {
		name        string
		grpcRoutes  []client.Object
		gateway     *gwtypes.Gateway
		expected    []gwtypes.GRPCRoute
		expectedErr bool
	}{
		{
			name: "returns GRPCRoute for a Gateway",
			grpcRoutes: []client.Object{
				&gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: []gwtypes.GRPCRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "does not return GRPCRoute for a Gateway when it is not a parent",
			grpcRoutes: []client.Object{
				&gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: nil,
		},
		{
			name: "returns GRPCRoute when section name does match",
			grpcRoutes: []client.Object{
				&gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("grpc")),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "grpc",
							Port: 80,
						},
					},
				},
			},
			expected: []gwtypes.GRPCRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("grpc")),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "does not return GRPCRoute when section name does not match",
			grpcRoutes: []client.Object{
				&gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("grpc-1")),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "grpc",
							Port: 80,
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "does not return GRPCRoute when port does not match",
			grpcRoutes: []client.Object{
				&gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "grpc-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("grpc")),
									Port:        new(gwtypes.PortNumber(8080)),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "grpc",
							Port: 80,
						},
					},
				},
			},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.gateway).
				WithObjects(tc.grpcRoutes...).
				Build()
			routes, err := ListGRPCRoutesForGateway(t.Context(), cl, tc.gateway)
			if tc.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, tc.expected, routes)
			}
		})
	}
}

func TestListTCPRoutesForGateway(t *testing.T) {
	testCases := []struct {
		name        string
		tcpRoutes   []client.Object
		gateway     *gwtypes.Gateway
		expected    []gwtypes.TCPRoute
		expectedErr bool
	}{
		{
			name: "returns TCPRoute for a Gateway",
			tcpRoutes: []client.Object{
				&gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: []gwtypes.TCPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "does not return TCPRoute for a Gateway when it is not a parent",
			tcpRoutes: []client.Object{
				&gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: nil,
		},
		{
			name: "does not return TCPRoute referencing a same-named Gateway in another namespace",
			tcpRoutes: []client.Object{
				&gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "other-namespace",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  new(gwtypes.Kind("Gateway")),
									// Namespace omitted: defaults to the route's namespace
									// (other-namespace), which must not match a Gateway in
									// "default" even when the name is identical.
									Name: gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: nil,
		},
		{
			name: "returns TCPRoute referencing the Gateway by explicit namespace",
			tcpRoutes: []client.Object{
				&gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "other-namespace",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:     new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:      new(gwtypes.Kind("Gateway")),
									Namespace: new(gwtypes.Namespace("default")),
									Name:      gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
			},
			expected: []gwtypes.TCPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "other-namespace",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:     new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:      new(gwtypes.Kind("Gateway")),
									Namespace: new(gwtypes.Namespace("default")),
									Name:      gwtypes.ObjectName("gw-1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "returns TCPRoute when section name and port match",
			tcpRoutes: []client.Object{
				&gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("tcp")),
									Port:        new(gwtypes.PortNumber(8888)),
								},
							},
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Gateway",
					APIVersion: gwtypes.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gw-1",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "tcp",
							Port: 8888,
						},
					},
				},
			},
			expected: []gwtypes.TCPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "tcp-route-1",
						Namespace:       "default",
						ResourceVersion: "1",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group:       new(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        new(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: new(gwtypes.SectionName("tcp")),
									Port:        new(gwtypes.PortNumber(8888)),
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.gateway).
				WithObjects(tc.tcpRoutes...).
				Build()
			routes, err := ListTCPRoutesForGateway(t.Context(), cl, tc.gateway)
			if tc.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, tc.expected, routes)
			}
		})
	}
}
