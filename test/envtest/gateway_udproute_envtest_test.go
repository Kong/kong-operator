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

func TestGatewayUDPRouteAttachedRoutes(t *testing.T) {
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
		ObjectMeta: metav1.ObjectMeta{Name: "gc-udproute"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, c.Create(ctx, gc))

	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, tickTime)

	backendService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: ns.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:     "dns",
				Protocol: corev1.ProtocolUDP,
				Port:     53,
			}},
		},
	}
	require.NoError(t, c.Create(ctx, backendService))

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "gw-udproute",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gc.Name),
			Listeners: []gatewayv1.Listener{{
				Name:          "udp",
				Protocol:      gatewayv1.UDPProtocolType,
				Port:          9999,
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
				return listener.Name == "udp"
			})
			if !ok {
				t.Logf("failed to find udp listener status in Gateway %s/%s", gw.Namespace, gw.Name)
				return false
			}

			if listener.AttachedRoutes != expectedAttachedRoutes {
				t.Logf("listener attached routes = %d, want %d", listener.AttachedRoutes, expectedAttachedRoutes)
				return false
			}

			if !lo.ContainsBy(listener.SupportedKinds, func(routeKind gatewayv1.RouteGroupKind) bool {
				return routeKind.Kind == gatewayv1.Kind("UDPRoute")
			}) {
				t.Logf("listener supported kinds = %#v, want to include UDPRoute", listener.SupportedKinds)
				return false
			}

			return true
		}
	}

	require.Eventually(t, assertGatewayListenerStatus(0), waitTime, tickTime)

	servicePort := gatewayv1.PortNumber(53)
	udpRoute := &gatewayv1.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "udp-route",
		},
		Spec: gatewayv1.UDPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gw.Name),
				}},
			},
			Rules: []gatewayv1.UDPRouteRule{{
				BackendRefs: []gatewayv1.BackendRef{{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: gatewayv1.ObjectName(backendService.Name),
						Port: &servicePort,
					},
				}},
			}},
		},
	}
	require.NoError(t, c.Create(ctx, udpRoute))

	require.Eventually(t, assertGatewayListenerStatus(1), waitTime, tickTime)

	// Attach a second UDPRoute to the same listener. Per GEP-2645 the older
	// route wins arbitration at the DataPlane, but both routes count toward
	// listener.AttachedRoutes — Listener.Status reflects every route whose
	// ParentRef targets the listener, regardless of conflict outcome.
	udpRouteSecond := &gatewayv1.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "udp-route-second",
		},
		Spec: gatewayv1.UDPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gw.Name),
				}},
			},
			Rules: []gatewayv1.UDPRouteRule{{
				BackendRefs: []gatewayv1.BackendRef{{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: gatewayv1.ObjectName(backendService.Name),
						Port: &servicePort,
					},
				}},
			}},
		},
	}
	require.NoError(t, c.Create(ctx, udpRouteSecond))

	require.Eventually(t, assertGatewayListenerStatus(2), waitTime, tickTime)

	// Delete the original (winner) UDPRoute. The remaining route stays
	// attached and gets promoted to winner; AttachedRoutes drops to 1.
	require.NoError(t, c.Delete(ctx, udpRoute))

	require.Eventually(t, assertGatewayListenerStatus(1), waitTime, tickTime)

	require.NoError(t, c.Delete(ctx, udpRouteSecond))

	require.Eventually(t, assertGatewayListenerStatus(0), waitTime, tickTime)
}
