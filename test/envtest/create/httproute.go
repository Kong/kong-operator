package create

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/test/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// HTTPRoutes creates a number of dummy HTTPRoutes for the given Gateway.
func HTTPRoutes(
	ctx context.Context,
	t *testing.T,
	ctrlClient ctrlclient.Client,
	gw gwtypes.Gateway,
	numOfRoutes int,
) []*gwtypes.HTTPRoute {
	svc := &corev1.Service{
		Name:      "backend-svc",
		Namespace: gw.Namespace,
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, svc))
	t.Cleanup(func() { _ = ctrlClient.Delete(ctx, svc) })

	routes := make([]*gwtypes.HTTPRoute, 0, numOfRoutes)
	for range numOfRoutes {
		httpPort := gwtypes.PortNumber(80)
		pathMatchPrefix := gwtypes.PathMatchPathPrefix
		httpRoute := &gwtypes.HTTPRoute{
			GenerateName: "httproute-",
			Namespace:    gw.Namespace,
			Spec: gwtypes.HTTPRouteSpec{
				CommonRouteSpec: gwtypes.CommonRouteSpec{
					ParentRefs: []gwtypes.ParentReference{{
						Name: gwtypes.ObjectName(gw.Name),
					}},
				},
				Rules: []gwtypes.HTTPRouteRule{{
					Matches: []gwtypes.HTTPRouteMatch{
						{
							Path: &gwtypes.HTTPPathMatch{
								Type:  &pathMatchPrefix,
								Value: new("/test-http-route"),
							},
						},
					},
					BackendRefs: []gwtypes.HTTPBackendRef{{
						Name: gwtypes.ObjectName("backend-svc"),
						Port: &httpPort,
						Kind: util.StringToGatewayAPIKindPtr("Service"),
					}},
				}},
			},
		}

		require.NoError(t, ctrlClient.Create(ctx, httpRoute))
		t.Cleanup(func() { _ = ctrlClient.Delete(ctx, httpRoute) })
		routes = append(routes, httpRoute)
	}
	return routes
}
