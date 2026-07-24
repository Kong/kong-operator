package integration

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration"
)

// tcpListenerPort is the Gateway listener port for the TCPRoute test.
// tcpEchoPort (the backend port) is shared with tlsroute_test.go.
const tcpListenerPort = 8888

// setTCPListener returns a Gateway option that configures a "tcp" listener on
// the given port with protocol TCP and no restrictions on route namespaces.
func setTCPListener(port int) func(*gatewayv1.Gateway) {
	tcpListener := gatewayv1.Listener{
		Name:     "tcp",
		Port:     gatewayv1.PortNumber(port),
		Protocol: gatewayv1.TCPProtocolType,
		AllowedRoutes: &gatewayv1.AllowedRoutes{
			Namespaces: &gatewayv1.RouteNamespaces{
				From: new(gatewayv1.NamespacesFromAll),
			},
		},
	}
	return func(gw *gatewayv1.Gateway) {
		for i, listener := range gw.Spec.Listeners {
			if listener.Name == "tcp" {
				gw.Spec.Listeners[i] = tcpListener
				return
			}
		}
		gw.Spec.Listeners = append(gw.Spec.Listeners, tcpListener)
	}
}

// hasRouteConditionTrue reports whether conds contains a condition of the given
// type with Status == True.
func hasRouteConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func TestTCPRoute(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := clients.OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = clients.GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, setTCPListener(tcpListenerPort))
	t.Logf("deploying Gateway %s/%s with a TCP listener on port %d", gateway.Namespace, gateway.Name, tcpListenerPort)
	gateway, err = clients.GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Accepted", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying tcpecho backend deployment")
	container := generators.NewContainer("tcpecho", testutils.TCPEchoImage, tcpEchoPort)
	container.Env = []corev1.EnvVar{
		{
			Name:  "POD_NAME",
			Value: "test-tcp-echo",
		},
		{
			Name:  "TCP_PORT",
			Value: strconv.Itoa(tcpEchoPort),
		},
	}
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing tcpecho deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	service, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("generating a TCPRoute")
	tcpRoute := helpers.GenerateTCPRoute(namespace.Name, gateway.Name, service.Name, tcpEchoPort)

	t.Logf("creating tcproute %s/%s to access deployment %s via kong", tcpRoute.Namespace, tcpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := clients.GatewayClient.GatewayV1().TCPRoutes(namespace.Name).Create(ctx, tcpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy tcproute %s/%s", tcpRoute.Namespace, tcpRoute.Name)
			cleaner.Add(result)
			tcpRoute = result
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Logf("verifying TCPRoute %s/%s has Accepted, ResolvedRefs and Programmed conditions set to True", tcpRoute.Namespace, tcpRoute.Name)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got, err := clients.GatewayClient.GatewayV1().TCPRoutes(namespace.Name).Get(ctx, tcpRoute.Name, metav1.GetOptions{})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotEmpty(c, got.Status.Parents, "TCPRoute has no parent statuses yet") {
			return
		}
		conds := got.Status.Parents[0].Conditions
		// gateway-api standardizes Accepted and ResolvedRefs on routes; the Programmed
		// condition on routes is a Kong convention set by ensureParentsProgrammedCondition
		// (Type == "Programmed").
		assert.Truef(c, hasRouteConditionTrue(conds, string(gatewayv1.RouteConditionAccepted)), "expected %s=True on TCPRoute, got %+v", gatewayv1.RouteConditionAccepted, conds)
		assert.Truef(c, hasRouteConditionTrue(conds, string(gatewayv1.RouteConditionResolvedRefs)), "expected %s=True on TCPRoute, got %+v", gatewayv1.RouteConditionResolvedRefs, conds)
		assert.Truef(c, hasRouteConditionTrue(conds, "Programmed"), "expected Programmed=True on TCPRoute, got %+v", conds)
	}, testutils.DefaultTCPRouteWait, testutils.WaitTCPRouteTick,
		"TCPRoute %s/%s did not get Accepted, ResolvedRefs and Programmed conditions set to True, current status: %+v",
		tcpRoute.Namespace, tcpRoute.Name, tcpRoute.Status)

	t.Logf("verifying connectivity to the TCPRoute via %s:%d", gatewayIPAddress, tcpListenerPort)
	require.Eventually(t, func() bool {
		err := helpers.EchoResponds(t, helpers.ProtocolTCP, fmt.Sprintf("%s:%d", gatewayIPAddress, tcpListenerPort), "test-tcp-echo")
		if err != nil {
			t.Logf("failed to access TCPRoute on %s:%d, error %+v", gatewayIPAddress, tcpListenerPort, err)
			return false
		}
		return true
	}, testutils.DefaultTCPRouteWait, testutils.WaitTCPRouteTick)
}
