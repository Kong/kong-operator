package integration

import (
	"fmt"
	"net/http"
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
	"github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/helpers/envs"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestHTTPRoute(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
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

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name)
	t.Logf("creating httproute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := integration.GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(ctx, httpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy httproute %s/%s", httpRoute.Namespace, httpRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	request := helpers.MustBuildRequest(t, ctx, http.MethodGet, "http://"+gatewayIPAddress+"/test", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
		httpRouteAccessTimeout,
		time.Second,
	)

	t.Log("route to /test/1234 path of service httpbin should receive a 404 OK response")
	request = helpers.MustBuildRequest(t, ctx, http.MethodGet, "http://"+gatewayIPAddress+"/test/1234", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<h1>Not Found</h1>"),
		httpRouteAccessTimeout,
		time.Second,
	)
}

func TestHTTPRouteWithTLS(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	const host = "integration.tests.org"
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithDNSNames(host))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      host,
			Labels: map[string]string{
				config.DefaultSecretLabelSelector: config.LabelValueForSelectorTrue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	t.Logf("deploying Secret %s/%s", secret.Namespace, secret.Name)
	secret, err = integration.GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, func(gateway *gatewayv1.Gateway) {
		gateway.Spec.Listeners[0].Protocol = gatewayv1.HTTPSProtocolType
		gateway.Spec.Listeners[0].Port = gatewayv1.PortNumber(443)
		gateway.Spec.Listeners[0].TLS = &gatewayv1.ListenerTLSConfig{
			CertificateRefs: []gatewayv1.SecretObjectReference{
				{
					Name:      gatewayv1.ObjectName(secret.Name),
					Namespace: new(gatewayv1.Namespace(secret.Namespace)),
				},
			},
		}
	})

	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Scheduled", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying httpbin backend deployment")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing httpbin deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name, func(h *gatewayv1.HTTPRoute) {
		h.Spec.Hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(host)}
	})

	t.Logf("creating httproute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := integration.GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(ctx, httpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy httproute %s/%s", httpRoute.Namespace, httpRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	httpClient := helpers.MustCreateHTTPClient(t, secret, host)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	request := helpers.MustBuildRequest(t, ctx, http.MethodGet, "https://"+gatewayIPAddress+"/test", host)
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
		httpRouteAccessTimeout,
		time.Second,
	)
	t.Log("route to /test/1234 path of service httpbin should receive a 404 OK response")
	request = helpers.MustBuildRequest(t, ctx, http.MethodGet, "https://"+gatewayIPAddress+"/test/1234", host)
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<h1>Not Found</h1>"),
		httpRouteAccessTimeout,
		time.Second,
	)
}

func TestHTTPRouteExpressionsRouterPortIsolation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	t.Log("prepare GatewayConfiguration with expressions router set")
	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	container := k8sutils.GetPodContainerByName(
		&gatewayConfig.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec,
		consts.DataPlaneProxyContainerName,
	)
	require.NotNil(t, container)
	container.Env = envs.SetValueByName(container.Env, consts.RouterFlavorEnvKey, string(consts.RouterFlavorExpressions))

	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	const (
		listenerHTTP          = "http"
		listenerHTTP2port8080 = "http2"
		port8080              = 8080
	)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, func(gateway *gatewayv1.Gateway) {
		gateway.Spec.Listeners[0].Name = listenerHTTP
		gateway.Spec.Listeners = append(gateway.Spec.Listeners,
			gatewayv1.Listener{
				Name:     listenerHTTP2port8080,
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(port8080),
			},
		)
	})
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
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

	t.Log("deploying backend deployment (httpbin) shared by both HTTPRoutes")
	deployment := generators.NewDeploymentForContainer(generators.NewContainer("httpbin", testutils.HTTPBinImage, 80))
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	const (
		pathOnPort80   = "/on-80"
		pathOnPort8080 = "/on-8080"
	)

	t.Logf("creating an HTTPRoute attached to the %q listener (port 80), matching %s", listenerHTTP, pathOnPort80)
	httpRoutePort80 := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name, func(h *gatewayv1.HTTPRoute) {
		h.Spec.ParentRefs[0].SectionName = new(gatewayv1.SectionName(listenerHTTP))
		h.Spec.Rules[0].Matches[0].Path.Value = new(pathOnPort80)
	})
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			r, err := integration.GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(ctx, httpRoutePort80, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy HTTPRoute %s/%s", httpRoutePort80.Namespace, httpRoutePort80.Name)
			cleaner.Add(r)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Logf("creating an HTTPRoute attached to the %q listener (port %d), matching %s", listenerHTTP2port8080, port8080, pathOnPort8080)
	httpRoutePort8080 := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name, func(h *gatewayv1.HTTPRoute) {
		h.Spec.ParentRefs[0].SectionName = new(gatewayv1.SectionName(listenerHTTP2port8080))
		h.Spec.Rules[0].Matches[0].Path.Value = new(pathOnPort8080)
	})
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			r, err := integration.GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(ctx, httpRoutePort8080, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy HTTPRoute %s/%s", httpRoutePort8080.Namespace, httpRoutePort8080.Name)
			cleaner.Add(r)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	t.Log("the route attached to the port-80 listener should be reachable on port 80")
	request := helpers.MustBuildRequest(t, ctx, http.MethodGet, fmt.Sprintf("http://%s:80%s", gatewayIPAddress, pathOnPort80), "")
	require.Eventually(t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
		waitTime, tickTime,
	)

	t.Logf("the route attached to the port-%d listener should be reachable on port %d", port8080, port8080)
	request = helpers.MustBuildRequest(t, ctx, http.MethodGet, fmt.Sprintf("http://%s:%d%s", gatewayIPAddress, port8080, pathOnPort8080), "")
	require.Eventually(t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
		waitTime, tickTime,
	)

	t.Logf("the route attached to the port-%d listener must not be reachable on port 80 (no route overlap across listener ports)", port8080)
	require.Eventually(t,
		asserts.Expect404WithNoRouteFunc(t, ctx, fmt.Sprintf("http://%s:80%s", gatewayIPAddress, pathOnPort8080)),
		waitTime, tickTime,
	)

	t.Logf("the route attached to the port-80 listener must not be reachable on port %d (no route overlap across listener ports)", port8080)
	require.Eventually(t,
		asserts.Expect404WithNoRouteFunc(t, ctx, fmt.Sprintf("http://%s:%d%s", gatewayIPAddress, port8080, pathOnPort80)),
		waitTime, tickTime,
	)
}
