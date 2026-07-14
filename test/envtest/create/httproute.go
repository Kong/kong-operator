package create

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util"
)

// HTTPRoutes creates a number of dummy HTTPRoutes for the given Gateway.
func HTTPRoutes(
	ctx context.Context,
	t *testing.T,
	ctrlClient ctrlclient.Client,
	gw gatewayapi.Gateway,
	numOfRoutes int,
) []*gatewayapi.HTTPRoute {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-svc",
			Namespace: gw.Namespace,
		},
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

	routes := make([]*gatewayapi.HTTPRoute, 0, numOfRoutes)
	for range numOfRoutes {
		httpPort := gatewayapi.PortNumber(80)
		pathMatchPrefix := gatewayapi.PathMatchPathPrefix
		httpRoute := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "httproute-",
				Namespace:    gw.Namespace,
			},
			Spec: gatewayapi.HTTPRouteSpec{
				CommonRouteSpec: gatewayapi.CommonRouteSpec{
					ParentRefs: []gatewayapi.ParentReference{{
						Name: gatewayapi.ObjectName(gw.Name),
					}},
				},
				Rules: []gatewayapi.HTTPRouteRule{{
					Matches: []gatewayapi.HTTPRouteMatch{
						{
							Path: &gatewayapi.HTTPPathMatch{
								Type:  &pathMatchPrefix,
								Value: new("/test-http-route"),
							},
						},
					},
					BackendRefs: []gatewayapi.HTTPBackendRef{{
						BackendRef: gatewayapi.BackendRef{
							BackendObjectReference: gatewayapi.BackendObjectReference{
								Name: gatewayapi.ObjectName("backend-svc"),
								Port: &httpPort,
								Kind: util.StringToGatewayAPIKindPtr("Service"),
							},
						},
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
