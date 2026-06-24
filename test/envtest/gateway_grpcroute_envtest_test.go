package envtest

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kogateway "github.com/kong/kong-operator/v2/controller/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util/builder"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func TestGatewayGRPCRouteAttachedRoutes(t *testing.T) {
	t.Parallel()

	const (
		waitTime = 30 * time.Second
		tickTime = 500 * time.Millisecond
	)

	ctx := t.Context()
	scheme := managerscheme.Get()

	cfg, ns := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             ns.Name,
		DefaultDataPlaneImage: "kong:latest",
	}
	StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()

	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc-grpcroute"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, c.Create(ctx, gc))

	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, tickTime)

	backendService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpcbin",
			Namespace: ns.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:     "grpc",
				Protocol: corev1.ProtocolTCP,
				Port:     80,
			}},
		},
	}
	require.NoError(t, c.Create(ctx, backendService))

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "gw-grpcroute",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gc.Name),
			Listeners: []gatewayv1.Listener{{
				Name:          "http",
				Protocol:      gatewayv1.HTTPProtocolType,
				Port:          80,
				AllowedRoutes: builder.NewAllowedRoutesFromAllNamespaces(),
			}},
		},
	}
	require.NoError(t, c.Create(ctx, gw))

	assertGatewayListenerStatus := func(expectedAttachedRoutes int32) func() bool {
		return func() bool {
			var current gatewayv1.Gateway
			if err := c.Get(ctx, client.ObjectKeyFromObject(gw), &current); err != nil {
				t.Logf("failed to get Gateway %s/%s: %v", gw.Namespace, gw.Name, err)
				return false
			}

			listener, ok := lo.Find(current.Status.Listeners, func(listener gatewayv1.ListenerStatus) bool {
				return listener.Name == "http"
			})
			if !ok {
				t.Logf("failed to find http listener status in Gateway %s/%s", gw.Namespace, gw.Name)
				return false
			}

			if listener.AttachedRoutes != expectedAttachedRoutes {
				t.Logf("listener attached routes = %d, want %d", listener.AttachedRoutes, expectedAttachedRoutes)
				return false
			}

			if !lo.ContainsBy(listener.SupportedKinds, func(routeKind gatewayv1.RouteGroupKind) bool {
				return routeKind.Kind == gatewayv1.Kind("GRPCRoute")
			}) {
				t.Logf("listener supported kinds = %#v, want to include GRPCRoute", listener.SupportedKinds)
				return false
			}

			return true
		}
	}

	require.Eventually(t, assertGatewayListenerStatus(0), waitTime, tickTime)

	servicePort := gatewayv1.PortNumber(80)
	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "grpc-route",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gw.Name),
				}},
			},
			Rules: []gatewayv1.GRPCRouteRule{{
				Matches: []gatewayv1.GRPCRouteMatch{{
					Method: &gatewayv1.GRPCMethodMatch{
						Service: new("grpcbin.GRPCBin"),
						Method:  new("DummyUnary"),
					},
				}},
				BackendRefs: []gatewayv1.GRPCBackendRef{{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName(backendService.Name),
							Port: &servicePort,
						},
					},
				}},
			}},
		},
	}
	require.NoError(t, c.Create(ctx, grpcRoute))

	require.Eventually(t, assertGatewayListenerStatus(1), waitTime, tickTime)

	require.NoError(t, c.Delete(ctx, grpcRoute))

	require.Eventually(t, assertGatewayListenerStatus(0), waitTime, tickTime)
}
