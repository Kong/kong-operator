package gateway

import (
	"testing"

	"github.com/samber/lo"
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
									Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
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
									Group: lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
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
									Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: lo.ToPtr(gwtypes.SectionName("http")),
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
									Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: lo.ToPtr(gwtypes.SectionName("http")),
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
									Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: lo.ToPtr(gwtypes.SectionName("http-1")),
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
									Group:       lo.ToPtr(gwtypes.Group(gwtypes.GroupVersion.Group)),
									Kind:        lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:        gwtypes.ObjectName("gw-1"),
									SectionName: lo.ToPtr(gwtypes.SectionName("http")),
									Port:        lo.ToPtr(gwtypes.PortNumber(8080)),
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
				require.Equal(t, tc.expected, routes)
			}
		})
	}
}
