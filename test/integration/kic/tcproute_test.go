package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	ktfkong "github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/kong/kong-operator/v2/ingress-controller/test"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
)

var tcpMutex = sync.Mutex{}

func TestTCPRouteReferenceGrant(t *testing.T) {
	ctx := t.Context()
	RunWhenKongExpressionRouter(t.Context(), t)
	t.Log("locking TCP port")
	tcpMutex.Lock()
	t.Cleanup(func() {
		t.Log("unlocking TCP port")
		tcpMutex.Unlock()
	})

	ns, cleaner := helpers.Setup(ctx, t, env)

	otherNs, err := clusters.GenerateNamespace(ctx, env.Cluster(), t.Name())
	require.NoError(t, err)

	gatewayClient, err := gatewayclient.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)

	t.Log("deploying a gatewayclass to the test cluster")
	gatewayClassName := uuid.NewString()
	gwc, err := helpers.DeployGatewayClass(ctx, gatewayClient, gatewayClassName)
	require.NoError(t, err)
	cleaner.Add(gwc)

	const gatewayTCPPortName = "tcp"

	t.Log("deploying a gateway to the test cluster using unmanaged gateway mode")
	gatewayName := uuid.NewString()
	gateway, err := helpers.DeployGateway(ctx, gatewayClient, ns.Name, gatewayClassName, func(gw *gwtypes.Gateway) {
		gw.Name = gatewayName
		gw.Spec.Listeners = []gwtypes.Listener{{
			Name:     gatewayTCPPortName,
			Protocol: gwtypes.TCPProtocolType,
			Port:     gwtypes.PortNumber(ktfkong.DefaultTCPServicePort),
		}}
	})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("creating a tcpecho pod to test TCPRoute traffic routing")
	container1 := generators.NewContainer("tcpecho-1", test.EchoImage, test.EchoTCPPort)
	// go-echo sends a "Running on Pod <UUID>." immediately on connecting
	testUUID1 := uuid.NewString()
	container1.Env = []corev1.EnvVar{
		{
			Name:  "POD_NAME",
			Value: testUUID1,
		},
	}
	deployment1 := generators.NewDeploymentForContainer(container1)
	deployment1, err = env.Cluster().Client().AppsV1().Deployments(ns.Name).Create(ctx, deployment1, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(deployment1)

	t.Log("creating an additional tcpecho pod to test TCPRoute multiple backendRef loadbalancing")
	container2 := generators.NewContainer("tcpecho-2", test.EchoImage, test.EchoTCPPort)
	// go-echo sends a "Running on Pod <UUID>." immediately on connecting
	testUUID2 := uuid.NewString()
	container2.Env = []corev1.EnvVar{
		{
			Name:  "POD_NAME",
			Value: testUUID2,
		},
	}
	deployment2 := generators.NewDeploymentForContainer(container2)
	deployment2, err = env.Cluster().Client().AppsV1().Deployments(otherNs.Name).Create(ctx, deployment2, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(deployment2)

	t.Logf("exposing deployment %s/%s via service", deployment1.Namespace, deployment1.Name)
	service1 := generators.NewServiceForDeployment(deployment1, corev1.ServiceTypeLoadBalancer)
	// Use the same port as the default TCP port from the Kong Gateway deployment
	// to the tcpecho port, as this is what will be used to route the traffic at the Gateway.
	const service1Port = ktfkong.DefaultTCPServicePort
	service1.Spec.Ports = []corev1.ServicePort{{
		Name:       "tcp",
		Protocol:   corev1.ProtocolTCP,
		Port:       service1Port,
		TargetPort: intstr.FromInt(test.EchoTCPPort),
	}}
	service1, err = env.Cluster().Client().CoreV1().Services(ns.Name).Create(ctx, service1, metav1.CreateOptions{})
	assert.NoError(t, err)
	cleaner.Add(service1)

	t.Logf("exposing deployment %s/%s via service", deployment2.Namespace, deployment2.Name)
	const service2Port = 8080 // Use a different port that listening on the Gateway for TCP.
	service2 := generators.NewServiceForDeployment(deployment2, corev1.ServiceTypeLoadBalancer)
	service2.Spec.Ports = []corev1.ServicePort{{
		Name:       "tcp",
		Protocol:   corev1.ProtocolTCP,
		Port:       service2Port,
		TargetPort: intstr.FromInt(test.EchoTCPPort),
	}}
	service2, err = env.Cluster().Client().CoreV1().Services(otherNs.Name).Create(ctx, service2, metav1.CreateOptions{})
	assert.NoError(t, err)
	cleaner.Add(service2)

	t.Logf("creating a tcproute to access deployment %s via kong", deployment1.Name)
	remoteNamespace := gwtypes.Namespace(otherNs.Name)
	tcproute := &gwtypes.TCPRoute{
		Name: uuid.NewString(),
		Spec: gwtypes.TCPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{
					Name:        gwtypes.ObjectName(gatewayName),
					SectionName: new(gwtypes.SectionName(gatewayTCPPortName)),
				}},
			},
			Rules: []gwtypes.TCPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{
					{
						Name: gwtypes.ObjectName(service1.Name),
						Port: new(gwtypes.PortNumber(service1Port)),
					},
					{
						Name:      gwtypes.ObjectName(service2.Name),
						Namespace: &remoteNamespace,
						Port:      new(gwtypes.PortNumber(service2Port)),
					},
				},
			}},
		},
	}
	tcproute, err = gatewayClient.GatewayV1().TCPRoutes(ns.Name).Create(ctx, tcproute, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(tcproute)

	t.Log("verifying that only the local tcpecho is responding without a ReferenceGrant")
	require.Eventually(t, func() bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID1) == nil
	}, ingressWait*2, waitTick)
	asserts.Never(t, func(context.Context) bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID2) == nil
	}, time.Second*10, time.Second)

	t.Logf("creating a ReferenceGrant that permits tcproute access from %s to services in %s", ns.Name, otherNs.Name)
	grant := &gwtypes.ReferenceGrant{
		Name: uuid.NewString(),
		Spec: gwtypes.ReferenceGrantSpec{
			From: []gwtypes.ReferenceGrantFrom{
				{
					// this isn't actually used, it's just a dummy extra from to confirm we handle multiple fine
					Group:     gwtypes.Group("gateway.networking.k8s.io"),
					Kind:      gwtypes.Kind("TCPRoute"),
					Namespace: gwtypes.Namespace("garbage"),
				},
				{
					Group:     gwtypes.Group("gateway.networking.k8s.io"),
					Kind:      gwtypes.Kind("TCPRoute"),
					Namespace: gwtypes.Namespace(tcproute.Namespace),
				},
			},
			To: []gwtypes.ReferenceGrantTo{
				// also a dummy
				{
					Group: gwtypes.Group(""),
					Kind:  gwtypes.Kind("Pterodactyl"),
				},
				{
					Group: gwtypes.Group(""),
					Kind:  gwtypes.Kind("Service"),
				},
			},
		},
	}

	grant, err = gatewayClient.GatewayV1().ReferenceGrants(otherNs.Name).Create(ctx, grant, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Log("verifying that requests reach both the local and remote namespace echo instances")
	require.Eventually(t, func() bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID1) == nil
	}, ingressWait, waitTick)
	require.Eventually(t, func() bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID2) == nil
	}, ingressWait, waitTick)

	t.Logf("testing specific name references")
	serviceName := gwtypes.ObjectName(service2.Name)
	grant.Spec.To[1] = gwtypes.ReferenceGrantTo{
		Kind:  gwtypes.Kind("Service"),
		Group: gwtypes.Group(""),
		Name:  &serviceName,
	}

	grant, err = gatewayClient.GatewayV1().ReferenceGrants(otherNs.Name).Update(ctx, grant, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID2) == nil
	}, ingressWait*2, waitTick)

	t.Logf("testing incorrect name does not match")
	blueguyName := gwtypes.ObjectName("blueguy")
	grant.Spec.To[1].Name = &blueguyName
	_, err = gatewayClient.GatewayV1().ReferenceGrants(otherNs.Name).Update(ctx, grant, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return helpers.EchoResponds(t, helpers.ProtocolTCP, proxyTCPURL, testUUID2) != nil
	}, ingressWait, waitTick)
}
