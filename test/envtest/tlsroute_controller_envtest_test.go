package envtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
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
	tlsRouteWaitDuration = 5 * time.Second
	tlsRouteTickDuration = 100 * time.Millisecond
)

func TestTLSRouteReconcilerTranslatesAndUpdatesProgrammedCondition(t *testing.T) {
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
			Name:      "backend-tls",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "tls",
					Protocol:   corev1.ProtocolTCP,
					Port:       443,
					TargetPort: intstr.FromInt(8443),
				},
			},
		},
	}
	require.NoError(t, client.Create(ctx, &svc))

	dataplane := mocks.Dataplane{KubernetesObjectReportsEnabled: true}
	dataplane.SetObjectStatus(ns.Name, "tlsroute-1", "Succeeded")

	reconciler := &gateway.TLSRouteReconciler{
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

	mode := gatewayapi.TLSModePassthrough
	gw := gatewayapi.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "gateway-tls",
		},
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: gatewayapi.ObjectName(gwc.Name),
			Listeners: []gatewayapi.Listener{
				{
					Name:     gatewayapi.SectionName("tls"),
					Port:     gatewayapi.PortNumber(443),
					Protocol: gatewayapi.TLSProtocolType,
					TLS: &gatewayapi.GatewayTLSConfig{
						Mode: &mode,
					},
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
				Name: gatewayapi.SectionName("tls"),
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
						Group: lo.ToPtr(gatewayapi.Group(gatewayv1.GroupVersion.Group)),
						Kind:  gatewayapi.Kind("TLSRoute"),
					},
				},
			},
		},
	}
	require.NoError(t, client.Status().Patch(ctx, &gw, ctrlclient.MergeFrom(gwOld)))

	route := gatewayapi.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "tlsroute-1",
		},
		Spec: gatewayapi.TLSRouteSpec{
			Hostnames: []gatewayapi.Hostname{"app.example.com"},
			CommonRouteSpec: gatewayapi.CommonRouteSpec{
				ParentRefs: []gatewayapi.ParentReference{
					{
						Name:      gatewayapi.ObjectName(gw.Name),
						Namespace: lo.ToPtr(gatewayapi.Namespace(ns.Name)),
					},
				},
			},
			Rules: []gatewayapi.TLSRouteRule{
				{
					BackendRefs: []gatewayapi.BackendRef{
						{
							BackendObjectReference: gatewayapi.BackendObjectReference{
								Kind: lo.ToPtr(gatewayapi.Kind("Service")),
								Name: gatewayapi.ObjectName(svc.Name),
								Port: lo.ToPtr(gatewayapi.PortNumber(443)),
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, client.Create(ctx, &route))

	nn := k8stypes.NamespacedName{Namespace: route.Namespace, Name: route.Name}
	containsAcceptedAndProgrammed := helpers.TLSRouteEventuallyContainsConditions(ctx, t, client, nn,
		metav1.Condition{
			Type:   string(gatewayapi.RouteConditionAccepted),
			Status: metav1.ConditionTrue,
		},
		metav1.Condition{
			Type:   "Programmed",
			Status: metav1.ConditionTrue,
		},
	)

	if !assert.Eventually(t, containsAcceptedAndProgrammed, tlsRouteWaitDuration, tlsRouteTickDuration) {
		t.Fatal(printTLSRouteConditions(ctx, client, nn))
	}

	assert.True(t, dataplane.KubernetesObjectIsConfigured(&route))
}

func printTLSRouteConditions(ctx context.Context, client ctrlclient.Client, nn k8stypes.NamespacedName) string {
	route := &gatewayapi.TLSRoute{}
	if err := client.Get(ctx, nn, route); err != nil {
		return fmt.Sprintf("failed to get TLSRoute %s: %v", nn.String(), err)
	}

	conditions := make([]string, 0, len(route.Status.Parents)*2)
	for _, parent := range route.Status.Parents {
		for _, cond := range parent.Conditions {
			conditions = append(conditions, fmt.Sprintf("parent=%s type=%s status=%s reason=%s", parent.ParentRef.Name, cond.Type, cond.Status, cond.Reason))
		}
	}

	if len(conditions) == 0 {
		return "TLSRoute has no parent conditions"
	}

	return fmt.Sprintf("TLSRoute conditions: %v", conditions)
}
