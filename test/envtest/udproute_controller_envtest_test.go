package envtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/test/controllers/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/helpers"
	"github.com/kong/kong-operator/v2/ingress-controller/test/mocks"
)

const (
	udpRouteWaitDuration = 5 * time.Second
	udpRouteTickDuration = 100 * time.Millisecond
)

func TestUDPRouteReconcilerTranslatesAndUpdatesProgrammedCondition(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := Scheme(t, WithGatewayAPI, WithKong)
	cfg, _ := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	client := NewControllerClient(t, scheme, cfg)

	ns := CreateNamespace(ctx, t, client)

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "backend-udp",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "udp",
					Protocol:   corev1.ProtocolUDP,
					Port:       53,
					TargetPort: intstr.FromInt(5353),
				},
			},
		},
	}
	require.NoError(t, client.Create(ctx, &svc))

	dataplane := mocks.Dataplane{KubernetesObjectReportsEnabled: true}
	// Two routes attached to the same listener — under GEP-2645 only the
	// winner is rendered into the dataplane, but BOTH routes are registered
	// as successfully translated, so the reconciler must propagate
	// Programmed=True to both.
	dataplane.SetObjectStatus(ns.Name, "udproute-1", "Succeeded")
	dataplane.SetObjectStatus(ns.Name, "udproute-2", "Succeeded")

	reconciler := &gateway.UDPRouteReconciler{
		Client:          client,
		DataplaneClient: dataplane,
	}
	StartReconciler(ctx, t, client.Scheme(), cfg, reconciler)

	gwc := gatewayapi.GatewayClass{
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: gateway.GetControllerName(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/gatewayclass-unmanaged": "placeholder",
			},
		},
	}
	require.NoError(t, client.Create(ctx, &gwc))
	t.Cleanup(func() { _ = client.Delete(ctx, &gwc) })

	gw := gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "gateway-udp",
		},
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: gatewayapi.ObjectName(gwc.Name),
			Listeners: []gatewayapi.Listener{
				{
					Name:     gatewayapi.SectionName("udp"),
					Port:     gatewayapi.PortNumber(53),
					Protocol: gatewayapi.UDPProtocolType,
				},
			},
		},
	}
	require.NoError(t, client.Create(ctx, &gw))

	gwOld := gw.DeepCopy()
	gw.Status = gatewayapi.GatewayStatus{
		Conditions: []metav1.Condition{
			{
				Type:               string(gatewayapi.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayapi.GatewayReasonProgrammed),
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: gw.Generation,
			},
			{
				Type:               string(gatewayapi.GatewayConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             "Accepted",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: gw.Generation,
			},
		},
		Listeners: []gatewayapi.ListenerStatus{
			{
				Name: gatewayapi.SectionName("udp"),
				Conditions: []metav1.Condition{
					{
						Type:               "Accepted",
						Status:             metav1.ConditionTrue,
						Reason:             "Accepted",
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               string(gatewayapi.ListenerConditionProgrammed),
						Status:             metav1.ConditionTrue,
						Reason:             string(gatewayapi.ListenerReasonProgrammed),
						LastTransitionTime: metav1.Now(),
					},
				},
				SupportedKinds: []gatewayapi.RouteGroupKind{
					{
						Group: new(gatewayapi.Group(gatewayv1.GroupVersion.Group)),
						Kind:  gatewayapi.Kind("UDPRoute"),
					},
				},
			},
		},
	}
	require.NoError(t, client.Status().Patch(ctx, &gw, ctrlclient.MergeFrom(gwOld)))

	mkRoute := func(name string) gatewayapi.UDPRoute {
		return gatewayapi.UDPRoute{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "gateway.networking.k8s.io/v1",
				Kind:       "UDPRoute",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.Name,
				Name:      name,
			},
			Spec: gatewayapi.UDPRouteSpec{
				CommonRouteSpec: gatewayapi.CommonRouteSpec{
					ParentRefs: []gatewayapi.ParentReference{
						{
							Name:      gatewayapi.ObjectName(gw.Name),
							Namespace: new(gatewayapi.Namespace(ns.Name)),
						},
					},
				},
				Rules: []gatewayapi.UDPRouteRule{
					{
						BackendRefs: []gatewayapi.BackendRef{
							{
								BackendObjectReference: gatewayapi.BackendObjectReference{
									Kind: new(gatewayapi.Kind("Service")),
									Name: gatewayapi.ObjectName(svc.Name),
									Port: new(gatewayapi.PortNumber(53)),
								},
							},
						},
					},
				},
			},
		}
	}

	// Create two UDPRoutes attached to the same listener so the test also
	// covers GEP-2645: the reconciler must mark BOTH routes as
	// Accepted=True/Programmed=True, even though only the winner is rendered
	// to the dataplane.
	route1 := mkRoute("udproute-1")
	require.NoError(t, client.Create(ctx, &route1))
	// Ensure a strictly newer CreationTimestamp on the second route so winner
	// arbitration is deterministic if a later assertion ever depends on it.
	time.Sleep(time.Second)
	route2 := mkRoute("udproute-2")
	require.NoError(t, client.Create(ctx, &route2))

	expectAcceptedAndProgrammed := func(nn k8stypes.NamespacedName) {
		t.Helper()
		fn := helpers.UDPRouteEventuallyContainsConditions(ctx, t, client, nn,
			metav1.Condition{
				Type:   string(gatewayapi.RouteConditionAccepted),
				Status: metav1.ConditionTrue,
				Reason: "Accepted",
			},
			metav1.Condition{
				Type:   "Programmed",
				Status: metav1.ConditionTrue,
				Reason: "ConfiguredInGateway",
			},
		)
		if !assert.Eventually(t, fn, udpRouteWaitDuration, udpRouteTickDuration) {
			t.Fatal(printUDPRouteConditions(ctx, client, nn))
		}
	}

	expectAcceptedAndProgrammed(k8stypes.NamespacedName{Namespace: route1.Namespace, Name: route1.Name})
	expectAcceptedAndProgrammed(k8stypes.NamespacedName{Namespace: route2.Namespace, Name: route2.Name})

	assert.True(t, dataplane.KubernetesObjectIsConfigured(&route1), "winner should be reported as configured")
	assert.True(t, dataplane.KubernetesObjectIsConfigured(&route2), "loser should also be reported as configured (translator registers both)")
}

func printUDPRouteConditions(ctx context.Context, client ctrlclient.Client, nn k8stypes.NamespacedName) string {
	route := &gatewayapi.UDPRoute{}
	if err := client.Get(ctx, nn, route); err != nil {
		return fmt.Sprintf("failed to get UDPRoute %s: %v", nn.String(), err)
	}

	conditions := make([]string, 0, len(route.Status.Parents)*2)
	for _, parent := range route.Status.Parents {
		for _, cond := range parent.Conditions {
			conditions = append(conditions, fmt.Sprintf("parent=%s type=%s status=%s reason=%s", parent.ParentRef.Name, cond.Type, cond.Status, cond.Reason))
		}
	}

	if len(conditions) == 0 {
		return "UDPRoute has no parent conditions"
	}

	return fmt.Sprintf("UDPRoute conditions: %v", conditions)
}
